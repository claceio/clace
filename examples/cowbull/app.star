load("http.in", "http")

SERVICE_URL = "http://localhost:9999"


def create_game(req):
    level = req.Form["level"]
    ret = http.post(SERVICE_URL + "/api/create_game/" + level[0], headers={
        "X-Forwarded-For": req.RemoteIP
    })
    return clace.redirect(req.AppPath + "/game/" + ret.json()["GameId"])


def fetch_game(req, game_id):
    game_path = req.AppPath + "/game/" + game_id
    ret = http.get(SERVICE_URL + "/api/game/" + game_id)
    game = ret.json()
    if game.get("Error"):
        return clace.response(game, "invalid_game_id", code=404)
    clues = http.get(SERVICE_URL + "/api/game/" + game_id + "/clues")
    game["Clues"] = clues.json()
    game["GamePath"] = game_path
    return game


def game_handler(req):
    game_id = req.UrlParams["game_id"]
    return fetch_game(req, game_id)


def post_game_update(req, req_type):
    arg = None
    if req_type == "submit":
        if "guess" not in req.Form:
            return clace.response(req, "game_error_block", req, code=400)
        arg = req.Form["guess"][0]

    game_id = req.UrlParams["game_id"]
    api_url = SERVICE_URL + "/api/game/" + game_id + "/" + req_type
    if arg:
        api_url += "/" + arg
    ret = http.post(api_url).json()
    if ret.get("Error"):
        return clace.response(ret, "game_error_block", retarget="#gameErrorId")
    return fetch_game(req, game_id)


def create_challenge(req):
    level = req.Form["level"]
    challenge = http.post(
        SERVICE_URL + "/api/create_challenge/" + level[0]).json()
    if challenge.get("Error"):
        return clace.response(challenge, "invalid_challenge_id", code=404)
    return clace.redirect(req.AppPath + "/challenge/" + challenge["ChallengeId"])


def challenge_handler(req):
    challenge_id = req.UrlParams["challenge_id"]
    challenge = http.get(SERVICE_URL + "/api/challenge/" + challenge_id).json()
    if challenge.get("Error"):
        return clace.response(challenge, "invalid_challenge_id", code=404)
    games = http.get(SERVICE_URL + "/api/challenge/" + challenge_id + "/games")
    challenge["Games"] = games.json()
    return challenge


def play_challenge(req):
    challenge_id = req.UrlParams["challenge_id"]
    ret = http.post(SERVICE_URL + "/api/challenge/" +
                    challenge_id + "/play").json()
    if ret.get("Error"):
        return clace.response(ret, "invalid_challenge_id", code=404)

    return clace.redirect(req.AppPath + "/game/" + ret["GameId"])


def join(req):
    id = req.Form["id"]
    if id:
        id = id[0]
    if len(id) == 4:
        return clace.redirect(req.AppPath + "/challenge/" + id)
    else:
        return clace.redirect(req.AppPath + "/game/" + id)


app = clace.app("CowBull",
                custom_layout=True,
                pages=[
                    clace.page("/", "index.go.html"),

                    clace.page("/game", "",
                               method="POST", handler=create_game),
                    clace.page("/game/{game_id}", "game.go.html", "game_info_tmpl", handler=game_handler,
                               fragments=[
                                   clace.fragment(
                                       "submit", method="POST", handler=lambda req: post_game_update(req, "submit")),
                                   clace.fragment(
                                       "hint", method="POST", handler=lambda req: post_game_update(req, "hint")),
                                   clace.fragment(
                                       "resign", method="POST", handler=lambda req: post_game_update(req, "resign")),
                               ]),

                    clace.page("/challenge", "challenge.go.html",
                               method="POST", handler=create_challenge),
                    clace.page("/challenge/{challenge_id}", "challenge.go.html", "challenge_info_tmpl", handler=challenge_handler,
                               fragments=[
                                   clace.fragment(
                                       "play", method="POST", handler=play_challenge),
                               ]),

                    clace.page("/join", "", method="POST", handler=join),
                    clace.page("/help", "help.go.html"),
                ],
                permissions=[
                    clace.permission("http.in", "get"),
                    clace.permission("http.in", "post")
                ],
                style=clace.style("daisyui", themes=["bumblebee"])
                )
