load("proxy.in", "proxy")

app_name = ace.param("app_name", "Proxy App")
port = ace.param("port")

app = ace.app(app_name,
              routes=[
                  ace.proxy("/", proxy.config("http://localhost:" + port)),
              ],
              permissions=[
                  ace.permission("proxy.in", "config", [
                                 "http://localhost:" + port]),
              ],
              )
