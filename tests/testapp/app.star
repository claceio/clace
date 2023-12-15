
def handler(req):
    return {"k1": "v1"}


def json_handler(req):
    return {"val": "555"}


app = ace.app("TestApp",
              pages=[ace.page("/"),
                     ace.page("/test1", handler=json_handler, type='json')],
              permissions=[
                  # ace.permission("http.in", "get"),
              ])
