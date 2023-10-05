app = ace.app("TestApp",
              pages=[ace.page("/")])


def handler(req):
    return {"k1": "v1"}
