# Sample OpenRun App

This folder has a sample OpenRun app used for tests. More examples are in the [apps repo](https://github.com/openrundev/apps). A demo of these apps is running at [apps.demo.clace.io](https://apps.demo.clace.io/).

`async_action` is a sample of [async actions](../docs/content/docs/Actions.md#async-actions): each scenario (a stored table result, a streamed command to watch and cancel, a timeout, output and result size limits, a handler failure) is one action. It runs `sh` through the exec plugin, which the server's host command allowlist (empty by default) must permit: run the server with `allowed_exec_commands = ["sh"]` under `[security]` in `openrun.toml`, then `openrun app create --dev --approve ./examples/async_action /async_action`.
