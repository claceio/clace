load("container.in", "container")
load("http.in", "http")

def handler(req):
    v1 = http.get(ace.CONTAINER_URL).value.body()
    v2 = http.get(ace.CONTAINER_URL + "/test").value.body()
    return {"v1": v1, "v2": v2}

app = ace.app("Flask Http Test",
              routes=[
                  ace.api("/")
              ],
              container=container.config(container.AUTO, port=param.port),
              permissions=[
                  ace.permission("container.in", "config", [container.AUTO]),
                  ace.permission("http.in", "get")
              ]
       ) 
