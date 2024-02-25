load("fs.in", "fs")


def handler(req, files=False, find=False):
    dir = req.Query.get("dir")
    current = dir[0] if dir and dir[0] else "."

    # Get the absolute path for the current directory
    ret = fs.abs(current)
    if not ret:
        return {"Current": current, "Error": "fs.abs failed: " + ret.error}
    current = ret.value
    parent = ""
    if current != "/":
        ret = fs.abs(current + "/..")
        if ret.error:
            return {"Current": current, "Error": "fs.abs failed: " + ret.error}
        parent = ret.value

    if find:
        ret = fs.find(current, min_size=10 * 1024,
                      ignore_errors=True, limit=30)
        files = True
    else:
        ret = fs.list(current, recursive_size=not files, ignore_errors=True)
    if not ret:
        return {"Current": current, "Error": "fs operation failed: " + ret.error}

    # Descending sort on size, limit to 30 files
    entries = [info for info in ret.value if (
        files and not info["is_dir"]) or (not files and info["is_dir"])]
    entries = sorted(entries, key=lambda d: d["size"], reverse=True)[:30]
    for entry in entries:
        entry["size"] = entry["size"] / 1024 / 1024

    return {"Current": current, "Entries": entries, "Error": "", "MaxSize": entries[0]["size"] if entries else 0, "Files": files, "Parent": parent}


app = ace.app("Disk Usage",
              pages=[
                  ace.page("/", partial="du_table_block",
                           fragments=[ace.fragment("files", handler=lambda req: handler(req, files=True)),
                                      ace.fragment("find", handler=lambda req: handler(req, find=True))])
              ],
              permissions=[
                  ace.permission("fs.in", "abs"),
                  ace.permission("fs.in", "list"),
                  ace.permission("fs.in", "find"),
              ],
              style=ace.style(
                  "https://cdn.jsdelivr.net/npm/@picocss/pico@2/css/pico.min.css"),
              )
