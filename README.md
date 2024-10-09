<p align="center">
  <img src="https://clace.io/clace.png" alt="Clace-logo" width="240" />

  <p align="center">Web App Deployment Platform</p>
</p>

<p>
  <a href="https://github.com/claceio/clace/releases"><img src="https://img.shields.io/github/release/claceio/clace.svg" alt="Latest Release"></a>
  <a href="https://github.com/claceio/clace/actions"><img src="https://github.com/claceio/clace/workflows/CI/badge.svg" alt="Build Status"></a>
  <a href="https://app.codecov.io/github/claceio/clace"><img src="https://codecov.io/gh/claceio/clace/branch/main/graph/badge.svg" alt="Code Coverage"></a>
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

Clace is an Apache-2.0 licensed project building a web app development and deployment platform for internal tools. Clace allows easy and secure hosting of multiple web apps, in any language/framework, on a single machine. Clace is cross-platform (Linux/Windows/OSX) and provides a GitOps workflow for managing web apps.

Clace combines the functionality of a reverse proxy, a hypermedia based micro-framework and a container orchestrator (using Docker or Podman) in a single lightweight binary. After starting the Clace server and ensuring Docker or Podman is running, new apps can be installed in one command from GitHub source repo. Clace builds the image and starts the container lazily, on the first API call.

Clace can be used to develop any containerized web app on a development machine and then deploy the app on a shared server. Apps are deployed directly from the git repo, no build step required. Clace can be used to deploy Streamlit apps, adding OAuth authentication for access control across a team.

This repo hosts the source code for Clace server and client. The source for the documentation site [clace.io](https://clace.io) is in the [docs](https://github.com/claceio/docs) repo. App specifications, which are templates to build apps, are defined in the [appspecs](https://github.com/claceio/appspecs) repo.

<img alt="Clace intro gif" src="https://clace.io/intro_dark_small.gif"/>

## Features

Clace can be used to:

- Deploy containerized applications, Clace will build and manage the container lifecycle
- Build Hypermedia based applications using Starlark (no containers required)
- Hybrid approach, where the backend APIs are implemented in a container and a Hypermedia based UI is implemented in Clace

Clace supports the following for all apps:

- Atomic updates (all or none) across [multiple apps](https://clace.io/docs/applications/overview/#glob-pattern)
- [Staging mode](https://clace.io/docs/applications/lifecycle/#staging-apps) for app updates, to verify whether code and config changes work on prod before making them live.
- [Preview app](https://clace.io/docs/applications/lifecycle/#preview-apps) creation support, for trying out code changes.
- Support for [github integration](https://clace.io/docs/configuration/security/#private-repository-access), apps being directly deployed from github code.
- OAuth and SSO based [authentication](https://clace.io/docs/configuration/authentication/#oauth-authentication)
- Support for domain based and path based [routing](https://clace.io/docs/applications/routing/#request-routing) at the app level.

For containerized apps, Clace supports:

- Managing [image builds](https://clace.io/docs/quickstart/#containerized-applications), in dev and prod mode
- Passing [parameters](https://clace.io/docs/app/overview/#app-parameters) for the container
- Building apps from [spec](https://clace.io/docs/app/overview/#building-apps-from-spec), no code changes required in repo for [supported frameworks](https://github.com/claceio/appspecs) (Flask, Streamlit and repos having a Containerfile)

For building Hypermedia based apps, Clace supports:

- Automatic [error handling support](https://clace.io/docs/plugins/overview/#automatic-error-handling)
- Automatic creation of ECMAScript modules using [esbuild](https://esbuild.github.io/).
- Support for [TailwindCSS](https://tailwindcss.com/) and [DaisyUI](https://daisyui.com/) watcher integration.
- [Automatic SSL](https://clace.io/docs/configuration/networking/#enable-automatic-signed-certificate) certificate creation based on [certmagic](https://github.com/caddyserver/certmagic).
- Backend app code runs in a [security sandbox](https://clace.io/docs/applications/appsecurity/#security-model), with allowlist based permissions.
- [No build step](https://clace.io/docs/app/overview/#app-lifecycle), the development artifacts are ready for production use.
- Support for application data persistance using SQLite
- Virtual filesystem with [content hash based file names](https://clace.io/docs/app/templates/#static-function) backed by SQLite database, enabling aggressive static content caching.
- Brotli compression for static artifacts, HTTP early hints support for performance.

## Roadmap

The feature roadmap for Clace is:

- SQLite is used for metadata storage currently. Support for postgres is planned. This will be used to allow for horizontal scaling.
- Integration with secrets managers, to securely access secrets.
- Support for pausing app containers which are idle

## Setup

### Install

To install on OSX/Linux, run

```shell
curl -L https://clace.io/install.sh | sh
source $HOME/clhome/bin/clace.env
clace server start &
```

To install on Windows, run

```
pwsh -Command "iwr https://clace.io/install.ps1 -useb | iex"
```

Use powershell if pwsh is not available. Start a new command window (to get the updated ENV values) and run
`clace server start` to start the service.

To install apps, run

```
clace app create --approve github.com/claceio/apps/system/disk_usage /disk_usage
clace app create --approve github.com/claceio/apps/utils/bookmarks /book
```

The disk usage app is available at https://localhost:25223/disk_usage (use port 25222 for HTTP). admin is the username, use the password printed by the install script. The bookmark manager is available at https://localhost:25223/book. Add `--auth none` to the `app create` command to disable auth.

To install a containerized app, ensure either Docker or Podman is running and run

```
clace app create --spec image --approve --param image=nginx --param port=80 - nginxapp.localhost:/
clace app create --spec python-streamlit --param app_file=hello --branch master --approve github.com/streamlit/streamlit-example /streamlit_hello
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
go build -o $HOME/clace ./cmd/clace/
```

### Initial Configuration For Source Install

To use the clace service, you need an initial config file with the service password and a work directory. The below instructions assume you are using $HOME/clhome/clace.toml as the config file and $HOME/clhome as the work directory location.

- Create the clhome directory
- Create the clace.toml file, and create a randomly generate password for the **admin** user account

```shell
export CL_HOME=$HOME/clhome && mkdir $CL_HOME
cd $CL_HOME
mkdir config
git clone -C config https://github.com/claceio/appspecs
$HOME/clace password > $CL_HOME/clace.toml
```

This will print a random password on the screen, note that down as the password to use for accessing the applications.

### Start Service

To start the service, the CL_HOME environment variable has to point to the work directory location.

```shell
export CL_HOME=$HOME/clhome
$HOME/clace server start
```

Add the exports to your shell profile file. The service logs will be going to $CL_HOME/logs.

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

PRs welcome for bug fixes. Commit messages should [reference
bugs](https://docs.github.com/en/github/writing-on-github/autolinked-references-and-urls).

For feature enhancements, please first file a ticket with the `feature` label and discuss the change before working on the code changes.

The Google [go style guide](https://google.github.io/styleguide/go/guide) is used for Clace. For application behavior related fixes, refer the [app unit test cases](https://github.com/claceio/clace/tree/main/internal/app/tests). Those test run as part of regular unit tests `go test ./...`. For API related changes, Clace uses the [commander-cli](https://github.com/commander-cli/commander) library for [automated CLI tests](https://github.com/claceio/clace/tree/main/tests). To run the CLI test, run `CL_HOME=. tests/run_cli_tests.sh` from the clace home directory.

Thanks for all contributions!

<a href="https://github.com/claceio/clace/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=claceio/clace" />
</a>
