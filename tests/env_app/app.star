load("exec.in", "exec")

def handler(req):
   ret = exec.run("echo", ['{{secret "' + param.secret_provider + '" "' + param.secret_key + '"}}'])
   if ret.error:
       return "Error" + ret.error
   return ret.value

app = ace.app("test env",
              custom_layout=True,
              routes = [ace.api("/", type="TEXT")],
              permissions = [ace.permission("exec.in", "run", ["echo"])]
             )
