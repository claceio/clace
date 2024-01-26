load("exec.in", "exec")


def handler(req):
    dir = req.Query.get("dir")
    current = dir[0] if dir and dir[0] else "."

    # Run readlink -f to get the absolute path for the current directory
    ret = exec.run("readlink", ["-f", current], process_partial=True)
    if not ret:
        print("Failed to run readlink stderr " + ret.error)
        return {"Current": current,
                "Error": "readlink -f {current} failed with {error}".format(current=current, error=ret.error)}
    print("ret", ret)
    print("ret value", ret.value)
    current = ret.value[0].strip()

    args = ["-m", "-d", "1"]
    args.append(current)

    # run the du command, allow for partial results to handle permission errors on some dirs
    ret = exec.run("du", args, process_partial=True)
    if not ret:
        print("Failed to run du " + ret.error)
        return {"Current": current,
                "Error": "du -h {current} failed with {error}".format(current=current, error=ret.error)}

    # Parse the results
    dirs = []
    for line in ret.value:
        cols = line.split("\t", 1)
        dirs.append({"Size": int(cols[0]), "Dir": cols[1]})

    # Descending sort on size, limit to 20 dirs
    dirs = sorted(dirs, key=lambda d: d["Size"], reverse=True)[:20]
    if len(dirs) > 1 and dirs[1]["Dir"] == current:
        # swap current dir to the top (if not already), useful when a child is at same usage level as current
        dirs[0], dirs[1] = dirs[1], dirs[0]

    return {"Current": current, "Dirs": dirs, "Error": "", "MaxSize": dirs[0]["Size"] if dirs else 0}


app = ace.app("Disk Usage",
              pages=[ace.page("/", partial="du_table_block")],
              permissions=[
                  ace.permission("exec.in", "run", ["du"], type="READ"),
                  ace.permission("exec.in", "run", ["readlink"], type="READ")
              ],
              style=ace.style("https://unpkg.com/mvp.css@1.14.0/mvp.css"),
              )
