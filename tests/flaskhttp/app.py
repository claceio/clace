from flask import Flask

app = Flask(__name__)

@app.route("/")
def root():
    return "hello"

@app.route("/test")
def hello_world():
    return "testworld"
