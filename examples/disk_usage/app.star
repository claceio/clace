load("exec.in", "exec")

app = clace.app("Disk Usage",
                pages = [clace.page("/", block="du_table_block")],
                permissions = [clace.permission("exec.in", "run", ["du"])],
                style = clace.style("https://unpkg.com/mvp.css@1.14.0/mvp.css")
)

def handler(req):
    args = ["-m", "-d", "1"]
    dir = req["Query"].get("dir")
    parent = "."
    if dir and dir[0]:
        parent = dir[0]
    args.append(parent)

    # run the du command, allow for partial results to handle permission errors on some dirs
    ret = exec.run("du", args, process_partial=True)
    if ret.exit_code != 0:
        print ("Failed to run du stderr " + ret.stderr + "code" + ret.error)
        return {"Error": ret.error + ": " + ret.stderr}
    
    # Parse the results
    dirs = []
    for line in ret.lines:
        cols = line.split()
        dirs.append({"Size": int(cols[0]), "Dir": cols[1]})

    # Descending sort on size, limit to 20 dirs
    dirs = sorted(dirs, key=lambda d: d["Size"], reverse=True)[:20]
    return {"Parent": parent, "Dirs": dirs, "Error": ""}