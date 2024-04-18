
def handler(req):
    return {"k1": "v1"}


def json_handler(req):
    return {"val": "555"}


app = ace.app("TestApp",
              routes=[ace.html("/"),
                      ace.api("/test1", handler=json_handler)],
              permissions=[
                  # ace.permission("http.in", "get"),
              ])
