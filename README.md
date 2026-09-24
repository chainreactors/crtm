**fork from https://github.com/projectdiscovery/pdtm**

## Intro

<h4 align="center">Chainreactors's Open Source Tool Manager</h4>

<p align="center">
<a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/license-MIT-_red.svg"></a>
<a href="https://github.com/chainreactors/crtm/releases"><img src="https://img.shields.io/github/release/chainreactors/crtm"></a>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#usage">Usage</a> •
  <a href="#running-crtm">Running crtm</a> •

**crtm** is a simple and easy-to-use golang based tool for managing open source projects from Chainreactors.

</p>


## Installation


**`crtm`** requires **go1.19** to install successfully. Run the following command to install the latest version:

1. Install using go install -

```sh
go install -v github.com/chainreactors/crtm/cmd/crtm@latest
```

2. Install by downloading binary from https://github.com/chainreactors/crtm/releases

<table>
<tr>
<td>  

> **Notes**:

> - *Currently, projects are installed by downloading the released project binary. This means that projects can only be installed on the platforms for which binaries have been published.*
> - *The path $HOME/.crtm/go/bin is added to the $PATH variable by default*

</table>
</tr>
</td> 

## Usage: 


```console
crtm is a simple and easy-to-use golang based tool for managing open source projects from ProjectDiscovery

Usage:
  ./crtm [flags]

Flags:
CONFIG:
   -config string            cli flag configuration file (default "$HOME/.config/crtm/config.yaml")
   -bp, -binary-path string  custom location to download project binary (default "$HOME/.crtm/go/bin")

INSTALL:
   -i, -install string[]  install single or multiple project by name (comma separated)
   -ia, -install-all      install all the projects
   -ip, -install-path     append path to PATH environment variables

UPDATE:
   -u, -update string[]         update single or multiple project by name (comma separated)
   -ua, -update-all             update all the projects
   -up, -self-update            update crtm to latest version
   -duc, -disable-update-check  disable automatic crtm update check

REMOVE:
   -r, -remove string[]  remove single or multiple project by name (comma separated)
   -ra, -remove-all      remove all the projects
   -rp, -remove-path     remove path from PATH environment variables

DEBUG:
   -sp, -show-path          show the current binary path then exit
   -version                 show version of the project
   -v, -verbose             show verbose output
   -nc, -no-color           disable output content coloring (ANSI escape codes)
   -disable-changelog, -dc  disable release changelog in output
```

## Running crtm

```console
$ crtm -install-all
[INF] Current crtm version v0.0.1
[INF] installing gogo...
[INF] installed gogo v2.13.2 (latest)
[INF] installing spray...
[INF] installed spray v0.9.9 (latest)
[INF] installing zombie...
[INF] installed zombie v1.2.0 (latest)
``` 

## Bundles for Go applications

`crtm-bundle` downloads selected tools for an explicit target and packages their
executables with version and SHA-256 metadata. For example, create `tools.yaml`:

```yaml
id: my-application
tools:
  rg: "15.2.0"
```

Run the generator on the build host, then compile the application for the same target:

```sh
go run ./cmd/crtm-bundle -config tools.yaml -target linux/amd64 -output internal/toolbundle -package toolbundle
GOOS=linux GOARCH=amd64 go build -tags arsenal_embed ./cmd/my-application
```

The generated `EmbeddedBundle()` returns a `*pkg.Bundle`. Use the generated
`ToolSpec.ManagerOption(bundle)` to configure the manager, then call `manager.Prepare(ctx, bundle)`
when initializing the tool environment. Construction stays read-only; preparation
extracts missing tools without network access. Unmodified bundle-managed tools follow
application upgrades; explicit user installations and edits are retained.

Without `-package`, the command produces a standalone bundle directory. Open it with
`pkg.OpenBundle(os.DirFS(path))`; the same Source implementation reads `embed.FS`.
`pkg.BuildBundle` is the library entry point. Custom tools reuse the registry's
`custom_tools` definitions in the YAML file. Empty versions or `latest` resolve to
fixed releases during packaging. The bundle ID should remain stable between releases.

Bundle payloads contain executables only. Tools may still need their own external
templates, databases, or runtime libraries. Normal `UpdateTool` requests remote latest;
source integrity errors fail directly instead of falling back to another source.

Multiple applications can share an `arsenal.yaml` catalog of versions and
`custom_tools` definitions. Each release selects its tools and inherits versions:

```yaml
id: my-application
catalog: ../../arsenal.yaml
tools:
  rg:
```

Catalog paths are relative to the selection file. A nonempty version overrides
the catalog default. The generator resolves selections with `pkg.LoadBundleSpec`
and emits `bundle_spec_generated.go` for runtime requirements and installation.
Use `-metadata-only -package toolbundle` to regenerate this small, committed file
without downloading executables. Custom definitions work in remote-only builds
as well as embedded builds and never modify the user's configuration.

## Thanks

* https://github.com/projectdiscovery/pdtm ,  crtm modified from pdtm, thanks to pdtm's work
