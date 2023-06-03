load("exec.plugin", "exec")

MAX_ENTRIES = 20

app = clace.app("Disk Usage",
			custom_layout=False,
			pages = [
				clace.page("/*", block="du_table_block"),
			]
)


def handler(req):
    args = ["-m", "-d", "1"]
    folder = req["Query"].get("folder")
    if folder and folder[0]:
        args.extend(folder)
    ret = exec.run("du", args)
    print(ret)
    folders = []
    for line in ret.lines:
        split = line.split()
        folders.append({"Size": int(split[0]), "Folder": split[1]})

    folders = sorted(folders, key=lambda x: x["Size"], reverse=True)
    folders = folders[:MAX_ENTRIES]

    data = {"Parent": folder, "Folders": folders}
    return data


