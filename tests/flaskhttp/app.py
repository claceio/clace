from flask import Flask
import configparser

app = Flask(__name__)

@app.route("/")
def root():
    return "hello"

@app.route("/test")
def hello_world():
    config = configparser.ConfigParser()
    try:
        config.read('/var/data.ini')
    except Exception as e:
        return str(e)
    return config['DEFAULT']['PASSWD']
