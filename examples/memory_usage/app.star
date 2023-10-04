load("exec.in", "exec")


MAX_CHILD_COUNT = 100
MAX_TREE_DEPTH = 10


def memory_handler(req):
    ret = exec.run("ps", ["-eo", "pid,ppid,rss,command"])
    if ret.exit_code != 0:
        print("Failed to run ps stderr " + ret.stderr + "code" + ret.error)
        return {"Error": "ps failed with {error} : {stderr}".format(error=ret.error, stderr=ret.stderr)}

    # Parse the results
    processes = []
    first_line = True
    for line in ret.lines:
        if first_line:
            first_line = False
            continue
        cols = line.split(None, 3)
        processes.append({"pid": int(cols[0]), "ppid": int(cols[1]), "rss": int(
            int(cols[2])/1024), "command": cols[3]})

    id_process_map = {
        0: {"pid": 0, "ppid": -1, "rss": 0, "command": "root"}
    }
    child_map = {}
    for proc in processes:
        id_process_map[proc["pid"]] = proc
        if proc["ppid"] not in child_map:
            child_map[proc["ppid"]] = [proc["pid"]]
        else:
            child_map[proc["ppid"]].append(proc["pid"])

    memo = {}
    memory_usage = {}
    for proc in processes:
        memory_usage[proc["pid"]] = get_child_memory(
            proc["pid"], id_process_map, child_map, memo)

    max_depth = 10
    return {
        "name": "init: " + id_process_map[1]["command"][:40],
        "value": id_process_map[1]["rss"],
        "children": get_children(1, id_process_map, child_map, memory_usage, 0, max_depth)
    }


def get_child_memory(ppid, id_to_process, child_map, memo):
    if ppid in memo:
        return memo[ppid]
    child_pids = child_map.get(ppid, [])
    memory = id_to_process[ppid]["rss"]
    if not child_pids:
        return memory
    for pid in child_pids:
        memory += get_child_memory(pid, id_to_process, child_map, memo)
    return memory


def get_children(ppid, id_to_process, child_map, memory_usage, depth, max_depth):
    child_pids = child_map.get(ppid, [])
    if not child_pids or depth >= MAX_TREE_DEPTH:
        return {
            "name": "Pid " + str(ppid) + ": " + id_to_process[ppid]["command"][0:150],
            "value": id_to_process[ppid]["rss"]
        }

    depth += 1
    child_memory = [(pid, memory_usage[pid]) for pid in child_pids]
    sorted_child_memory = sorted(
        child_memory, key=lambda x: x[1], reverse=True)

    child_list = []
    for ct in sorted_child_memory[:MAX_CHILD_COUNT]:
        pid = ct[0]
        name = "Pid " + str(pid) + ": "
        command = id_to_process[pid]["command"]
        if len(command) > 40:
            command = command.split("/")[-1][0:40]
        name += command
        ret = get_children(
            pid, id_to_process, child_map, memory_usage, depth, max_depth)

        if type(ret) == "dict":
            child_list.append(ret)
        else:
            child_list.append({"name": name, "children": ret,
                              "value": id_to_process[pid]["rss"]})
    return child_list


app = ace.app("Memory Usage",
              custom_layout=True,
              pages=[
                  ace.page("/"),
                  ace.page("/memory", handler=memory_handler, type="json"),
              ],
              permissions=[
                  ace.permission("exec.in", "run", ["ps"]),
              ],
              libraries=[ace.library("d3", "7.8.5")],
              )
