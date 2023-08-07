load("exec.plugin", "exec")

app = clace.app("Disk Usage",
			custom_layout=False,
			pages = [
				clace.page("/", block="du_table_block"),
			]
)

def handler(req):
    args = ["-m", "-d", "1"]
    folder = req["Query"].get("folder")
    parent = "."
    if folder and folder[0]:
        parent = folder[0]
    args.append(parent)
    ret = exec.run("du", args)
    if ret.exit_code != 0:
        print ("Failed to run du " + ret.stderr + ret.error)
        return {"Error": "Failed to run du " + ret.stderr + ret.error}
    folders = []
    for line in ret.lines:
        cols = line.split()
        folders.append({"Size": int(cols[0]), "Folder": cols[1]})

    # Descending sort on size, limit to 20 folders
    folders = sorted(folders, key=lambda f: f["Size"], reverse=True)[:20]
    data = {"Parent": parent, "Folders": folders}
    return data


