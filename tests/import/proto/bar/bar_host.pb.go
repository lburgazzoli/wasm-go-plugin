//go:build !tinygo.wasm

// Code generated by protoc-gen-go-plugin. DO NOT EDIT.
// versions:
// 	protoc-gen-go-plugin v0.1.0
// 	protoc               v3.21.5
// source: tests/import/proto/bar/bar.proto

package bar

import (
	context "context"
	errors "errors"
	fmt "fmt"
	wazero "github.com/tetratelabs/wazero"
	api "github.com/tetratelabs/wazero/api"
	wasi_snapshot_preview1 "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	sys "github.com/tetratelabs/wazero/sys"
	io "io"
	fs "io/fs"
	os "os"
)

const BarPluginAPIVersion = 1

type BarPluginOption struct {
	Stdout io.Writer
	Stderr io.Writer
	FS     fs.FS
}

type BarPlugin struct {
	runtime wazero.Runtime
	config  wazero.ModuleConfig
}

func NewBarPlugin(ctx context.Context, opt BarPluginOption) (*BarPlugin, error) {

	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntime(ctx)

	// Combine the above into our baseline config, overriding defaults.
	config := wazero.NewModuleConfig().
		// By default, I/O streams are discarded and there's no file system.
		WithStdout(opt.Stdout).WithStderr(opt.Stderr).WithFS(opt.FS)

	return &BarPlugin{
		runtime: r,
		config:  config,
	}, nil
}
func (p *BarPlugin) Load(ctx context.Context, pluginPath string) (Bar, error) {
	b, err := os.ReadFile(pluginPath)
	if err != nil {
		return nil, err
	}

	// Create an empty namespace so that multiple modules will not conflict
	ns := p.runtime.NewNamespace(ctx)

	if _, err = wasi_snapshot_preview1.NewBuilder(p.runtime).Instantiate(ctx, ns); err != nil {
		return nil, err
	}

	// Compile the WebAssembly module using the default configuration.
	code, err := p.runtime.CompileModule(ctx, b)
	if err != nil {
		return nil, err
	}

	// InstantiateModule runs the "_start" function, WASI's "main".
	module, err := ns.InstantiateModule(ctx, code, p.config)
	if err != nil {
		// Note: Most compilers do not exit the module after running "_start",
		// unless there was an Error. This allows you to call exported functions.
		if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, fmt.Errorf("unexpected exit_code: %d", exitErr.ExitCode())
		} else if !ok {
			return nil, err
		}
	}

	// Compare API versions with the loading plugin
	apiVersion := module.ExportedFunction("bar_api_version")
	if apiVersion == nil {
		return nil, errors.New("bar_api_version is not exported")
	}
	results, err := apiVersion.Call(ctx)
	if err != nil {
		return nil, err
	} else if len(results) != 1 {
		return nil, errors.New("invalid bar_api_version signature")
	}
	if results[0] != BarPluginAPIVersion {
		return nil, fmt.Errorf("API version mismatch, host: %d, plugin: %d", BarPluginAPIVersion, results[0])
	}

	hello := module.ExportedFunction("bar_hello")
	if hello == nil {
		return nil, errors.New("bar_hello is not exported")
	}

	malloc := module.ExportedFunction("malloc")
	if malloc == nil {
		return nil, errors.New("malloc is not exported")
	}

	free := module.ExportedFunction("free")
	if free == nil {
		return nil, errors.New("free is not exported")
	}
	return &barPlugin{module: module,
		malloc: malloc,
		free:   free,
		hello:  hello,
	}, nil
}

type barPlugin struct {
	module api.Module
	malloc api.Function
	free   api.Function
	hello  api.Function
}

func (p *barPlugin) Hello(ctx context.Context, request Request) (response Reply, err error) {
	data, err := request.MarshalVT()
	if err != nil {
		return response, err
	}
	dataSize := uint64(len(data))
	results, err := p.malloc.Call(ctx, dataSize)
	if err != nil {
		return response, err
	}

	dataPtr := results[0]
	// This pointer is managed by TinyGo, but TinyGo is unaware of external usage.
	// So, we have to free it when finished
	defer p.free.Call(ctx, dataPtr)

	// The pointer is a linear memory offset, which is where we write the name.
	if !p.module.Memory().Write(ctx, uint32(dataPtr), data) {
		return response, fmt.Errorf("Memory.Write(%d, %d) out of range of memory size %d", dataPtr, dataSize, p.module.Memory().Size(ctx))
	}
	ptrSize, err := p.hello.Call(ctx, dataPtr, dataSize)
	if err != nil {
		return response, err
	}

	// Note: This pointer is still owned by TinyGo, so don't try to free it!
	resPtr := uint32(ptrSize[0] >> 32)
	resSize := uint32(ptrSize[0])

	// The pointer is a linear memory offset, which is where we write the name.
	bytes, ok := p.module.Memory().Read(ctx, resPtr, resSize)
	if !ok {
		return response, fmt.Errorf("Memory.Read(%d, %d) out of range of memory size %d",
			resPtr, resSize, p.module.Memory().Size(ctx))
	}

	if err = response.UnmarshalVT(bytes); err != nil {
		return response, err
	}

	return response, nil
}
