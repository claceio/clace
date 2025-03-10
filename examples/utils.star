
# Declarative app config, install by running
#    clace apply --approve github.com/claceio/clace/examples/utils.star

# Install couple of Hypermedia based apps
app("/utils/bookmarks", "github.com/claceio/apps/utils/bookmarks")
app("/utils/disk_usage", "github.com/claceio/apps/system/disk_usage")

# Install a proxy app and a static file app
app("clace.localhost:", "-", spec="proxy", params={"url": "https://clace.io"})
app("/misc/event_planner", "github.com/simonw/tools", spec="static_single", params={"index": "event-planner.html"})

# Install container based apps (python and go)
limits = {"cpus": "2", "memory": "512m"} # Set limits (optional)
app("/misc/streamlit_example", "github.com/streamlit/streamlit-example", git_branch="master",
    spec="python-streamlit", container_opts=limits)
app("fasthtml.localhost:", "github.com/AnswerDotAI/fasthtml/examples",
    spec="python-fasthtml",  params={"APP_MODULE":"basic_ws:app"}, container_opts=limits)
app("/misc/go_example", "github.com/golang/example/helloserver", git_branch="master",
    spec="go", params={"port": "8080", "APP_ARGS":"-addr 0.0.0.0:8080"}, container_opts=limits)
