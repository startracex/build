package compileropts

import (
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/tsgo/pkg/collections"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Resolution holds the tsconfig switches that steer module resolution. They
// decide which file a specifier points at, not the text emitted for it.
type Resolution struct {
	ModuleResolution                 core.ModuleResolutionKind
	Paths                            *collections.OrderedMap[string, []string]
	PathsBasePath                    tspath.RootedDirectoryPath
	ResolvePackageJsonExports        core.Tristate
	ResolvePackageJsonImports        core.Tristate
	CheckJs                          core.Tristate
	ForceConsistentCasingInFileNames core.Tristate
}

// ResolutionFrom reads the resolution switches. baseURL stands in for the
// removed tsconfig "baseUrl" option, so a tsconfig that still uses it keeps
// resolving bare specifiers against the project root.
//
// A nil source yields the zero value, which leaves the destination at its
// defaults.
func ResolutionFrom(src *core.CompilerOptions, baseURL resolve.BaseURLFallback) Resolution {
	if src == nil {
		return Resolution{}
	}
	return Resolution{
		ModuleResolution:                 resolve.NormalizeModuleResolution(src.ModuleResolution),
		Paths:                            resolve.WithPathsFallback(src.Paths, baseURL, src.PathsBasePath),
		PathsBasePath:                    src.PathsBasePath,
		ResolvePackageJsonExports:        src.ResolvePackageJsonExports,
		ResolvePackageJsonImports:        src.ResolvePackageJsonImports,
		CheckJs:                          src.CheckJs,
		ForceConsistentCasingInFileNames: src.ForceConsistentCasingInFileNames,
	}
}

// Apply copies the switches onto dst.
func (o Resolution) Apply(dst *core.CompilerOptions) {
	dst.ModuleResolution = o.ModuleResolution
	dst.Paths = o.Paths
	dst.PathsBasePath = o.PathsBasePath
	dst.ResolvePackageJsonExports = o.ResolvePackageJsonExports
	dst.ResolvePackageJsonImports = o.ResolvePackageJsonImports
	dst.CheckJs = o.CheckJs
	dst.ForceConsistentCasingInFileNames = o.ForceConsistentCasingInFileNames
}
