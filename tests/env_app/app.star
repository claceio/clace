load("exec.in", "exec")

def handler(req):
   ret = exec.run("echo", ['{{secret "' + param.secret_provider + '" "' + param.secret_key + '"}}'])
   if ret.error:
       return "Error" + ret.error
   return ret.value


def multi(req):
   ret = exec.run("echo", ['{{secret "' + param.secret_provider + '" "c1" "c2" "c3"}}'])
   if ret.error:
       return "Error" + ret.error
   return ret.value

app = ace.app("test env",
              custom_layout=True,
              routes = [ace.api("/", type="TEXT"), ace.api("/multi", type="TEXT", handler=multi)],
              permissions = [ace.permission("exec.in", "run", ["echo"])]
             )
