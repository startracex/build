package plugin

import "context"

// HookCtx carries context into plugin hooks.
type HookCtx struct {
	Context context.Context
}

// File is one emitted output file passed to GenerateBundle hooks.
type File struct {
	Path string
	Data string
}

// Plugin groups optional build hooks. Nil function fields are skipped.
type Plugin struct {
	GenerateBundle func(ctx HookCtx, files []File) ([]File, error)
	Load           func(ctx HookCtx, id string) (code string, ok bool, err error)
	Resolve        func(ctx HookCtx, source, importer string) (id string, external bool, ok bool, err error)
	Transform      func(ctx HookCtx, code, id string) (codeOut string, mapJSON string, err error)
	Name           string
}

// RunResolve calls Resolve on each plugin; the first ok result wins.
func RunResolve(plugins []Plugin, ctx HookCtx, source, importer string) (id string, external bool, ok bool, err error) {
	for _, p := range plugins {
		if p.Resolve == nil {
			continue
		}
		id, external, ok, err = p.Resolve(ctx, source, importer)
		if err != nil || ok {
			return
		}
	}
	return "", false, false, nil
}

// RunLoad calls Load on each plugin; the first ok result wins.
func RunLoad(plugins []Plugin, ctx HookCtx, id string) (code string, ok bool, err error) {
	for _, p := range plugins {
		if p.Load == nil {
			continue
		}
		code, ok, err = p.Load(ctx, id)
		if err != nil || ok {
			return
		}
	}
	return "", false, nil
}

// RunTransform runs Transform on every plugin that defines it, in order.
func RunTransform(plugins []Plugin, ctx HookCtx, code, id string) (string, string, error) {
	var mapJSON string
	for _, p := range plugins {
		if p.Transform == nil {
			continue
		}
		var err error
		code, mapJSON, err = p.Transform(ctx, code, id)
		if err != nil {
			return "", "", err
		}
	}
	return code, mapJSON, nil
}

// RunGenerateBundle runs GenerateBundle on every plugin that defines it, in order.
func RunGenerateBundle(plugins []Plugin, ctx HookCtx, files []File) ([]File, error) {
	var err error
	for _, p := range plugins {
		if p.GenerateBundle == nil {
			continue
		}
		files, err = p.GenerateBundle(ctx, files)
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
