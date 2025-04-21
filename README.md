<p align="center">
  <img src="https://clace.io/clace.png" alt="Clace-logo" width="240" />

  <p align="center">App deployment simplified, GitOps without the hassles.</p>
</p>

<p>
  <a href="https://github.com/claceio/clace/blob/main/LICENSE"><img src="https://img.shields.io/github/license/claceio/clace" alt="License"></a>
  <a href="https://github.com/claceio/clace/releases"><img src="https://img.shields.io/github/release/claceio/clace.svg?color=00C200" alt="Latest Release"></a>
  <a href="https://github.com/claceio/clace/actions"><img src="https://github.com/claceio/clace/workflows/CI/badge.svg" alt="Build Status"></a>
  <a href="https://app.codecov.io/github/claceio/clace"><img src="https://img.shields.io/codecov/c/github/claceio/clace" alt="Code Coverage"></a>
  <a href="https://goreportcard.com/report/github.com/claceio/clace"><img src="https://goreportcard.com/badge/github.com/claceio/clace" alt="Go Report Card"></a>
  <a href="https://github.com/avelino/awesome-go"><img src="https://awesome.re/mentioned-badge.svg" alt="Mentioned in Awesome Go"></a>
  <a href="https://landscape.cncf.io/?item=app-definition-and-development--application-definition-image-build--clace"><img src="https://img.shields.io/badge/CNCF%20Landscape-0086FF" alt="Listed in CNCF landscape"></a>
</p>

### Menu

- [Overview](#overview)
- [Features](#features)
  - [Development Features](#development-features)
  - [Deployment Features](#deployment-features)
- [Roadmap](#roadmap)
- [Setup](#setup)
  - [Build from source](#build-from-source)
  - [Initial Configuration](#initial-configuration)
  - [Start Service](#start-service)
  - [Loading Apps](#loading-apps)
- [Documentation](#documentation)
- [Getting help](#getting-help)
- [Contributing](#contributing)

## Overview

Clace is an Apache-2.0 licensed project building a web app development and deployment platform for internal tools. Clace allows you to deploy containerized apps and develop Hypermedia based web apps. Clace is cross-platform (Linux/Windows/OSX) and provides a GitOps workflow for managing web apps.

Clace apps are deployed directly from the git repo, no build step required. For example, Clace can be used to deploy Streamlit/Gradio apps, adding OAuth authentication for access control across a team.

This repo hosts the source code for Clace. The source for the documentation site [clace.io](https://clace.io) is in the [docs](https://github.com/claceio/docs) repo. App specifications, which are templates to create apps, are defined in the [appspecs](https://github.com/claceio/appspecs) repo. Sample apps are in the [apps](https://github.com/claceio/apps) repo.

<img alt="Clace intro gif" src="https://clace.io/intro_dark_small.gif"/>

## Features

Clace can be used to:

- Automatically generate a form based UI for backend [actions](https://clace.io/docs/actions/)
- Deploy [containerized applications](https://clace.io/docs/container/overview/), Clace will build and manage the container lifecycle
- Build and deploy custom [Hypermedia based applications](https://clace.io/docs/app/overview/) using Starlark (no containers required)

Clace supports the following for all apps:

- [Declarative](https://clace.io/docs/applications/overview/#declarative-app-management) app deployment
- Atomic updates (all or none) across [multiple apps](https://clace.io/docs/applications/overview/#glob-pattern)
- [Staging mode](https://clace.io/docs/applications/lifecycle/#staging-apps) for app updates, to verify whether code and config changes work on prod before making them live.
- [Preview app](https://clace.io/docs/applications/lifecycle/#preview-apps) creation support, for trying out code changes.
- Support for [github integration](https://clace.io/docs/configuration/security/#private-repository-access), apps being directly deployed from github code.
- OAuth and SSO based [authentication](https://clace.io/docs/configuration/authentication/#oauth-authentication)
- Support for domain based and path based [routing](https://clace.io/docs/applications/routing/#request-routing) at the app level.
- Integration with [secrets managers](https://clace.io/docs/configuration/secrets/), to securely access secrets.

For containerized apps, Clace supports:

- Managing [image builds](https://clace.io/docs/quickstart/#containerized-applications), in dev and prod mode
- Passing [parameters](https://clace.io/docs/develop/#app-parameters) for the container
- Building apps from [spec](https://clace.io/docs/develop/#building-apps-from-spec), no code changes required in repo for [supported frameworks](https://github.com/claceio/appspecs) (Flask, Streamlit and repos having a Containerfile)
- Support for [pausing](https://clace.io/docs/container/config/) app containers which are idle

For building Hypermedia based apps, Clace supports:

- Automatic [error handling support](https://clace.io/docs/plugins/overview/#automatic-error-handling)
- Automatic creation of ECMAScript modules using [esbuild](https://esbuild.github.io/).
- Support for [TailwindCSS](https://tailwindcss.com/) and [DaisyUI](https://daisyui.com/) watcher integration.
- [Automatic SSL](https://clace.io/docs/configuration/networking/#enable-automatic-signed-certificate) certificate creation based on [certmagic](https://github.com/caddyserver/certmagic).
- Backend app code runs in a [security sandbox](https://clace.io/docs/applications/appsecurity/#security-model), with allowlist based permissions.
- [No build step](https://clace.io/docs/develop/#app-lifecycle), the development artifacts are ready for production use.
- Support for application data persistance using SQLite
- Virtual filesystem with [content hash based file names](https://clace.io/docs/develop/templates/#static-function) backed by SQLite database, enabling aggressive static content caching.
- Brotli compression for static artifacts, HTTP early hints support for performance.

## Roadmap

The feature roadmap for Clace is:

- SQLite is used for metadata storage currently. Support for postgres is planned. This will be used to allow for horizontal scaling.

## Setup


### Certs and Default password

Clace manages TLS cert using LetsEncrypt for prod environments. For dev environment, it is recommended to install [mkcert](https://github.com/FiloSottile/mkcert). Clace will automatically create local certs using mkcert if it is present. Install mkcert and run `mkcert -install` before starting Clace server. Installing Clace using brew will automatically install mkcert.

For container based apps, Docker or Podman or Orbstack should be installed and running on the machine. Clace automatically detects the container manager to use.

Clace uses an `admin` user account as the default authentication for accessing apps. A random password is generated for this account during initial Clace server installation. Note down this password for accessing apps.

### Install Clace On OSX/Linux

To install on OSX/Linux, run

```shell
curl -sSL https://clace.io/install.sh | sh
```
Start a new terminal (to get the updated env) and run `clace server start` to start the Clace service.

### Brew Install

To install using brew, run

```
brew tap claceio/homebrew-clace
brew install clace
brew services start clace
```

### Install On Windows

To install on Windows, run

```
powershell -Command "iwr https://clace.io/install.ps1 -useb | iex"
```

Start a new command window (to get the updated env) and run `clace server start` to start the Clace service.

### Install Apps

Once Clace server is running, to install apps declaratively, open a new window and run

```
clace apply --approve github.com/claceio/clace/examples/utils.star all
```

To install apps using the CLI, run

```
clace app create --approve github.com/claceio/apps/system/list_files /files
clace app create --approve github.com/claceio/apps/system/disk_usage /disk_usage
clace app create --approve github.com/claceio/apps/utils/bookmarks /book
```

Open https://localhost:25223 to see the app listing. The disk usage app is available at https://localhost:25223/disk_usage (port 25222 for HTTP). admin is the username, use the password printed by the install script. The bookmark manager is available at https://localhost:25223/book, the list files app is available at https://localhost:25223/files. Add the `--auth none` flag to the `app create` command to disable authentication.

See [installation]({{< ref "installation" >}}) for details. See [config options]({{< ref "configuration" >}}) for configuration options. To enable Let's Encrypt certificates, see [Automatic SSL]({{< ref "configuration/networking/#enable-automatic-signed-certificate" >}}).

The release binaries are also available at [releases](https://github.com/claceio/clace/releases). See [install from source]({{< ref "installation/#install-from-source" >}}) to build from source.


To install a containerized app, ensure either Docker or Podman is running and run

```
clace app create --spec python-streamlit --branch master --approve github.com/streamlit/streamlit-example /streamlit
```

If the source repo has a `Dockerfile` or `Containerfile`, run

```
clace app create --spec container --approve <source_path> /myapp
```

to install the app.

### Build from source

To install a release build, follow steps in the [installation docs](https://clace.io/docs/installation/#install-release-build).

To install from source:

- Ensure that a recent version of [Go](https://go.dev/doc/install) is available, version 1.21.0 or newer
- Checkout the Clace repo, cd to the checked out folder
- Build the clace binary and place in desired location, like $HOME

```shell
# Ensure go is in the $PATH
mkdir $HOME/clace_source && cd $HOME/clace_source
git clone -b main https://github.com/claceio/clace && cd clace
export CL_HOME=$HOME/clhome && mkdir -p $CL_HOME/config
go build -o $CL_HOME/clace ./cmd/clace/
```

### Initial Configuration For Source Install

To use the clace service, you need an initial config file with the service password and a work directory. The below instructions assume you are using $HOME/clhome/clace.toml as the config file and $HOME/clhome as the work directory location.

- Create the clhome directory
- Create the clace.toml file, and create a randomly generate password for the **admin** user account

```shell
cd $CL_HOME
git clone -C config https://github.com/claceio/appspecs
$CL_HOME/clace password > $CL_HOME/clace.toml
$CL_HOME/clace server start
```

This will print a random password on the screen, note that down as the password to use for accessing the applications.
The service will be started on [https://localhost:25223](https://127.0.0.1:25223) by default (HTTP port 25222).

### Loading Apps

To create an app, run the Clace client

```shell
$HOME/clace app create --approve $HOME/clace_source/clace/examples/disk_usage/ /disk_usage
```

This will create an app at /disk_usage with the example disk_usage app. The disk_usage app provides a web interface for looking at file system disk usage, allowing the user to explore the sub-folders which are consuming most disk space.

To access the app, go to [https://127.0.0.1:25223/disk_usage](https://127.0.0.1:25223/disk_usage). Use `admin` as the username and use the password previously generated. Allow the browser to connect to the self-signed certificate page. Or connect to [http://127.0.0.1:25222/disk_usage](http://127.0.0.1:25222/disk_usage) to avoid the certificate related warning.

## Sample App

To create an app with a custom HTML page which shows a listing of files, create an directory `~/fileapp` with file `app.star` file containing:

```python
load("exec.in", "exec")

def handler(req):
   ret = exec.run("ls", ["-l"])
   if ret.error:
       return {"Error": ret.error, "Lines": []}
   return {"Error": "", "Lines": ret.value}

app = ace.app("File Listing",
              custom_layout=True,
              routes = [ace.html("/")],
              permissions = [ace.permission("exec.in", "run", ["ls"])]
             )
```

and file `index.go.html` containing:

<!-- prettier-ignore -->
```html
<!doctype html>
<html>
  <head>
    <title>File List</title>
  </head>
  <body>
    <h1>File List</h1>
    {{ .Data.Error }}
    {{ range .Data.Lines }}
       {{.}}
       <br/>
    {{end}}
  </body>
</html>
```

<!-- prettier-ignore-end -->

Run `clace app create --auth=none --approve ~/fileapp /files`. The app is available at `https://localhost:25223/files`.

## Documentation

Clace docs are at https://clace.io/docs/. For doc bugs, raise a GitHub issue in the [docs](https://github.com/claceio/docs) repo.

## Getting help

Please use [Github Discussions](https://github.com/claceio/clace/discussions) for discussing Clace related topics. Please use the bug tracker only for bug reports and feature requests.

## Contributing

PRs welcome for bug fixes. For feature enhancements, please first file a ticket with the `feature` label and discuss the change before working on the code changes.

The Google [go style guide](https://google.github.io/styleguide/go/guide) is used for Clace. For application behavior related fixes, refer the [app unit test cases](https://github.com/claceio/clace/tree/main/internal/app/tests). Those test run as part of regular unit tests `go test ./...`. For API related changes, Clace uses the [commander-cli](https://github.com/commander-cli/commander) library for [automated CLI tests](https://github.com/claceio/clace/tree/main/tests). To run the CLI test, run `gmake test` from the clace home directory.

Thanks for all contributions!

<a href="https://github.com/claceio/clace/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=claceio/clace" />
</a>
