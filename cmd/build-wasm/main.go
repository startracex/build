//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"io/fs"
	"syscall/js"
	"time"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/plugin"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
)

type jsFileInfo struct {
	modified time.Time
	name     string
	size     int64
	dir      bool
}

func (f *jsFileInfo) Name() string       { return f.name }
func (f *jsFileInfo) Size() int64        { return f.size }
func (f *jsFileInfo) Mode() fs.FileMode  { return 0 }
func (f *jsFileInfo) ModTime() time.Time { return f.modified }
func (f *jsFileInfo) IsDir() bool        { return f.dir }
func (f *jsFileInfo) Sys() any           { return nil }

// jsFS adapts a JS filesystem, defaulting missing methods to neutral answers so adapters need only build-relevant operations.
type jsFS struct {
	obj             js.Value
	caseSensitivity tspath.CaseSensitivity
}

func (f *jsFS) call(name string, args ...any) (js.Value, error) {
	fn := f.obj.Get(name)
	if fn.IsUndefined() || fn.IsNull() || fn.Type() != js.TypeFunction {
		return js.Undefined(), fmt.Errorf("fs.%s is not a function", name)
	}
	return callJS(fn, args...)
}

// callJS invokes a JS function and turns a thrown value into a Go error.
func callJS(fn js.Value, args ...any) (result js.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if jsErr, ok := r.(js.Error); ok {
				err = fmt.Errorf("%s", jsErr.Value.Get("message").String())
				return
			}
			err = fmt.Errorf("%v", r)
		}
	}()
	result = fn.Invoke(args...)
	return result, nil
}

func (f *jsFS) CaseSensitivity() tspath.CaseSensitivity { return f.caseSensitivity }

func (f *jsFS) FileExists(path tspath.RootedFilePath) bool {
	v, err := f.call("fileExists", path.AsString())
	return err == nil && v.Bool()
}

func (f *jsFS) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	v, err := f.call("directoryExists", path.AsString())
	return err == nil && v.Bool()
}

func (f *jsFS) ReadFile(path tspath.RootedFilePath) (string, bool) {
	v, err := f.call("readFile", path.AsString())
	if err != nil || v.IsUndefined() || v.IsNull() {
		return "", false
	}
	if v.Type() == js.TypeString {
		text := v.String()
		return text, text != ""
	}
	if v.Type() == js.TypeObject {
		buf := make([]byte, v.Get("length").Int())
		js.CopyBytesToGo(buf, v)
		return string(buf), true
	}
	return "", false
}

func (f *jsFS) WriteFile(path tspath.RootedFilePath, data string) error {
	_, err := f.call("writeFile", path.AsString(), data)
	return err
}

func (f *jsFS) AppendFile(path tspath.RootedFilePath, data string) error {
	_, err := f.call("appendFile", path.AsString(), data)
	return err
}

func (f *jsFS) Remove(path tspath.RootedPath) error {
	_, err := f.call("remove", path.AsString())
	return err
}

func (f *jsFS) Chtimes(path tspath.RootedPath, aTime, mTime time.Time) error {
	return nil
}

func (f *jsFS) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	v, err := f.call("entries", path.AsString())
	var entries vfs.Entries
	if err != nil || v.IsUndefined() || v.IsNull() {
		return entries
	}
	entries.Files = stringArray(v.Get("files"))
	entries.Directories = stringArray(v.Get("directories"))
	if symlinks := v.Get("symlinks"); symlinks.Type() == js.TypeObject {
		entries.Symlinks = map[string]struct{}{}
		for _, name := range stringArray(js.Global().Get("Object").Call("keys", symlinks)) {
			entries.Symlinks[name] = struct{}{}
		}
	}
	return entries
}

func (f *jsFS) Stat(path tspath.RootedPath) vfs.FileInfo {
	v, err := f.call("stat", path.AsString())
	if err != nil || v.IsUndefined() || v.IsNull() {
		return nil
	}
	info := &jsFileInfo{
		name:     stringField(v, "name"),
		size:     int64(numberField(v, "size")),
		dir:      boolField(v, "isDirectory"),
		modified: time.UnixMilli(int64(numberField(v, "mtimeMs"))),
	}
	if info.name == "" {
		info.name = tspath.GetBaseFileName(path.AsString())
	}
	return info
}

func (f *jsFS) Realpath(path tspath.RootedPath) tspath.RootedPath {
	v, err := f.call("realpath", path.AsString())
	if err != nil || v.IsUndefined() || v.IsNull() || v.Type() != js.TypeString {
		return path
	}
	resolved, ok := tspath.TryRootedPathFromAbsolute(v.String())
	if !ok {
		return path
	}
	return resolved
}

func stringField(obj js.Value, key string) string {
	v := obj.Get(key)
	if v.IsUndefined() || v.IsNull() {
		return ""
	}
	return v.String()
}

func boolField(obj js.Value, key string) bool {
	v := obj.Get(key)
	return !v.IsUndefined() && !v.IsNull() && v.Bool()
}

func numberField(obj js.Value, key string) float64 {
	v := obj.Get(key)
	if v.IsUndefined() || v.IsNull() {
		return 0
	}
	return v.Float()
}

func stringArray(v js.Value) []string {
	if v.IsUndefined() || v.IsNull() || v.Type() != js.TypeObject {
		return nil
	}
	out := make([]string, 0, v.Length())
	for i := range v.Length() {
		out = append(out, v.Index(i).String())
	}
	return out
}

func objectStringMap(v js.Value) map[string]string {
	if v.IsUndefined() || v.IsNull() || v.Type() != js.TypeObject {
		return nil
	}
	keys := js.Global().Get("Object").Call("keys", v)
	out := make(map[string]string, keys.Length())
	for i := range keys.Length() {
		key := keys.Index(i).String()
		out[key] = v.Get(key).String()
	}
	return out
}

func boolFieldOrNil(obj js.Value, key string) *bool {
	v := obj.Get(key)
	if v.IsUndefined() || v.IsNull() {
		return nil
	}
	b := v.Bool()
	return &b
}

func optionsFromJS(value js.Value) (bundler.Options, error) {
	opts := bundler.Options{
		Entries:           stringArray(value.Get("entries")),
		Format:            bundler.Format(stringField(value, "format")),
		JSX:               bundler.JSX(stringField(value, "jsx")),
		Mode:              bundler.Mode(stringField(value, "mode")),
		OutFile:           stringField(value, "outFile"),
		OutDir:            stringField(value, "outDir"),
		RootDir:           stringField(value, "rootDir"),
		NameTemplate:      stringField(value, "nameTemplate"),
		EntryFileNames:    stringField(value, "entryFileNames"),
		ChunkFileNames:    stringField(value, "chunkFileNames"),
		AssetFileNames:    stringField(value, "assetFileNames"),
		Declaration:       boolFieldOrNil(value, "declaration"),
		DeclarationMap:    boolFieldOrNil(value, "declarationMap"),
		SourceMap:         boolFieldOrNil(value, "sourceMap"),
		External:          stringArray(value.Get("external")),
		BundleNodeModules: boolField(value, "bundleNodeModules"),
		DepsDir:           stringField(value, "depsDir"),
		Conditions:        stringArray(value.Get("conditions")),
		GlobalName:        stringField(value, "globalName"),
		Define:            objectStringMap(value.Get("define")),
		Drop:              stringArray(value.Get("drop")),
		Alias:             objectStringMap(value.Get("alias")),
		ManualChunks:      objectStringMap(value.Get("manualChunks")),
		CacheDir:          stringField(value, "cacheDir"),
		Manifest:          boolField(value, "manifest"),
		PublicPath:        stringField(value, "publicPath"),
		CWD:               stringField(value, "cwd"),
	}

	if outputs := value.Get("outputs"); !outputs.IsUndefined() && !outputs.IsNull() && outputs.Type() == js.TypeObject {
		for i := range outputs.Length() {
			item := outputs.Index(i)
			opts.Outputs = append(opts.Outputs, bundler.OutputOptions{
				Format:         bundler.Format(stringField(item, "format")),
				OutFile:        stringField(item, "outFile"),
				OutDir:         stringField(item, "outDir"),
				Name:           stringField(item, "name"),
				NameTemplate:   stringField(item, "nameTemplate"),
				EntryFileNames: stringField(item, "entryFileNames"),
				ChunkFileNames: stringField(item, "chunkFileNames"),
				AssetFileNames: stringField(item, "assetFileNames"),
			})
		}
	}

	if adapter := value.Get("fs"); !adapter.IsUndefined() && !adapter.IsNull() {
		sensitivity := tspath.CaseSensitive
		if stringField(adapter, "caseSensitivity") == "insensitive" {
			sensitivity = tspath.CaseInsensitive
		}
		opts.FS = &jsFS{obj: adapter, caseSensitivity: sensitivity}
	}
	plugins, err := pluginsFromJS(value.Get("plugins"))
	if err != nil {
		return opts, err
	}
	opts.Plugins = plugins
	return opts, nil
}

func pluginsFromJS(value js.Value) ([]plugin.Plugin, error) {
	if value.IsUndefined() || value.IsNull() || value.Type() != js.TypeObject {
		return nil, nil
	}
	plugins := make([]plugin.Plugin, 0, value.Length())
	for i := range value.Length() {
		item := value.Index(i)
		p := plugin.Plugin{Name: stringField(item, "name")}
		if fn, ok := hookFn(item, "resolve"); ok {
			p.Resolve = func(_ plugin.HookCtx, source, importer string) (string, bool, bool, error) {
				v, err := callJS(fn, js.ValueOf(source), js.ValueOf(importer))
				if err != nil || v.IsUndefined() || v.IsNull() {
					return "", false, false, err
				}
				return stringField(v, "id"), boolField(v, "external"), boolField(v, "ok"), nil
			}
		}
		if fn, ok := hookFn(item, "load"); ok {
			p.Load = func(_ plugin.HookCtx, id string) (string, bool, error) {
				v, err := callJS(fn, js.ValueOf(id))
				if err != nil || v.IsUndefined() || v.IsNull() {
					return "", false, err
				}
				return stringField(v, "code"), true, nil
			}
		}
		if fn, ok := hookFn(item, "transform"); ok {
			p.Transform = func(_ plugin.HookCtx, code, id string) (string, *sourcemap.Map, error) {
				v, err := callJS(fn, js.ValueOf(code), js.ValueOf(id))
				if err != nil {
					return "", nil, err
				}
				if v.IsUndefined() || v.IsNull() {
					return code, nil, nil
				}
				m, err := sourcemap.ParseMap(stringField(v, "map"))
				if err != nil {
					return "", nil, err
				}
				return stringField(v, "code"), m, nil
			}
		}
		if fn, ok := hookFn(item, "generateBundle"); ok {
			p.GenerateBundle = func(_ plugin.HookCtx, files []plugin.File) ([]plugin.File, error) {
				items := make([]any, len(files))
				for j, file := range files {
					items[j] = map[string]any{"path": file.Path, "data": file.Data}
				}
				v, err := callJS(fn, js.ValueOf(items))
				if err != nil {
					return nil, err
				}
				out := make([]plugin.File, 0, v.Length())
				for j := range v.Length() {
					item := v.Index(j)
					out = append(out, plugin.File{Path: stringField(item, "path"), Data: stringField(item, "data")})
				}
				return out, nil
			}
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

func hookFn(obj js.Value, name string) (js.Value, bool) {
	fn := obj.Get(name)
	if fn.IsUndefined() || fn.IsNull() || fn.Type() != js.TypeFunction {
		return js.Value{}, false
	}
	return fn, true
}

func jsBuild(_ js.Value, args []js.Value) (out any) {
	defer func() {
		if r := recover(); r != nil {
			out = map[string]any{"error": fmt.Sprintf("build: %v", r)}
		}
	}()
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return map[string]any{"error": "build: missing options"}
	}
	opts, err := optionsFromJS(args[0])
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	result, err := bundler.Build(context.Background(), opts)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	files := make([]any, len(result.Files))
	for i, file := range result.Files {
		files[i] = map[string]any{"path": file.Path, "data": file.Data}
	}
	return map[string]any{
		"files":    files,
		"manifest": objectStringMapFromGo(result.Manifest),
		"sources":  objectStringMapFromGo(result.Sources),
		"entry":    result.JavaScript(),
	}
}

func objectStringMapFromGo(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var buildFunc = js.FuncOf(jsBuild)

func main() {
	js.Global().Set("@startracex/build", map[string]any{"build": buildFunc})
	select {}
}
