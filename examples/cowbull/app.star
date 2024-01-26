load("http.in", "http")

SERVICE_URL = "http://localhost:9999"


def create_game(req):
    level = req.Form["level"]
    ret = http.post(SERVICE_URL + "/api/create_game/" + level[0], headers={
        "X-Forwarded-For": req.RemoteIP
    })
    if not ret:
        return ace.response(ret.error)
    return ace.redirect(req.AppPath + "/game/" + ret.value.json()["GameId"])


def fetch_game(req, game_id):
    game_path = req.AppPath + "/game/" + game_id
    ret = http.get(SERVICE_URL + "/api/game/" + game_id)
    if not ret:
        return ace.response(ret.error, code=404)

    if ret.value.json().get("Error"):
        return ace.response(ret.value.json(), "invalid_game_id", code=404)

    game = ret.value.json()
    clues = http.get(SERVICE_URL + "/api/game/" + game_id + "/clues")
    if not clues:
        return ace.response(clues.error, code=404)
    game["Clues"] = clues.value.json()
    game["GamePath"] = game_path
    return game


def game_handler(req):
    game_id = req.UrlParams["game_id"]
    return fetch_game(req, game_id)


def post_game_update(req, req_type):
    arg = None
    if req_type == "submit":
        if "guess" not in req.Form:
            return ace.response(req, "game_error_block", code=400)
        arg = req.Form["guess"][0]

    game_id = req.UrlParams["game_id"]
    api_url = SERVICE_URL + "/api/game/" + game_id + "/" + req_type
    if arg:
        api_url += "/" + arg
    ret = http.post(api_url)
    if not ret:
        return ace.response(ret.error)
    error = ret.value.json().get("Error")
    if error:
        return ace.response(ret.value.json(), "game_error_block", retarget="#gameErrorId")
    return fetch_game(req, game_id)


def create_challenge(req):
    level = req.Form["level"]
    challenge = http.post(SERVICE_URL + "/api/create_challenge/" + level[0])
    if not challenge:
        return ace.response(challenge.value.json(), "invalid_challenge_id", code=404)
    return ace.redirect(req.AppPath + "/challenge/" + challenge.value.json()["ChallengeId"])


def challenge_handler(req):
    challenge_id = req.UrlParams["challenge_id"]
    challenge = http.get(SERVICE_URL + "/api/challenge/" + challenge_id)
    if not challenge:
        return ace.response(challenge.error, "invalid_challenge_id", code=404)

    ret = challenge.value.json()
    if ret.get("Error"):
        return ace.response(ret, "invalid_challenge_id", code=404)
    games = http.get(SERVICE_URL + "/api/challenge/" + challenge_id + "/games")
    ret["Games"] = games.value.json()
    return ret


def play_challenge(req):
    challenge_id = req.UrlParams["challenge_id"]
    ret = http.post(SERVICE_URL + "/api/challenge/" +
                    challenge_id + "/play", headers={
                        "X-Forwarded-For": req.RemoteIP
                    })
    if not ret:
        return ace.response(ret.error, "invalid_challenge_id", code=404)
    if ret.value.json().get("Error"):
        return ace.response(ret.value.json(), "invalid_challenge_id", code=404)

    return ace.redirect(req.AppPath + "/game/" + ret.value.json()["GameId"])


def join(req):
    id = req.Form["id"]
    if id:
        id = id[0]
    if len(id) == 4:
        return ace.redirect(req.AppPath + "/challenge/" + id)
    else:
        return ace.redirect(req.AppPath + "/game/" + id)


app = ace.app("CowBull",
              custom_layout=True,
              pages=[
                  ace.page("/", "index.go.html"),

                  ace.page("/game", "",
                           method="POST", handler=create_game),
                  ace.page("/game/{game_id}", "game.go.html", "game_info_tmpl", handler=game_handler,
                           fragments=[
                               ace.fragment(
                                   "submit", method="POST", handler=lambda req: post_game_update(req, "submit")),
                               ace.fragment(
                                   "hint", method="POST", handler=lambda req: post_game_update(req, "hint")),
                               ace.fragment(
                                   "resign", method="POST", handler=lambda req: post_game_update(req, "resign")),
                           ]),

                  ace.page("/challenge", "challenge.go.html",
                           method="POST", handler=create_challenge),
                  ace.page("/challenge/{challenge_id}", "challenge.go.html", "challenge_info_tmpl", handler=challenge_handler,
                           fragments=[
                               ace.fragment(
                                   "play", method="POST", handler=play_challenge),
                           ]),

                  ace.page("/join", "", method="POST", handler=join),
                  ace.page("/help", "help.go.html"),
              ],
              permissions=[
                  ace.permission("http.in", "get"),
                  ace.permission("http.in", "post")
              ],
              style=ace.style("daisyui", themes=["bumblebee"])
              )
