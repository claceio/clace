load("proxy.in", "proxy")
load("container.in", "container")

app = ace.app(param.app_name,
              routes=[
                  ace.proxy("/", proxy.config(ace.container))
              ],
              container=container.config(port=param.port),
              permissions=[
                  ace.permission("proxy.in", "config", [ace.container]),
                  ace.permission("container.in", "config")
              ]
              )
