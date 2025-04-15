from flask import Flask
import configparser

config = configparser.ConfigParser()
config.read('/var/data.ini')

app = Flask(__name__)

@app.route("/")
def root():
    return "hello"

@app.route("/test")
def hello_world():
    return config['DEFAULT']['PARAM1']
