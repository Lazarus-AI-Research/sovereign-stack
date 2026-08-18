from sovereign_evals.smoke.checks import chat_completion


class Response:
    def __init__(self, body, status_code=200):
        self.body = body
        self.status_code = status_code
        self.text = str(body)

    def json(self):
        return self.body


class Client:
    def __init__(self, response, requests):
        self.response = response
        self.requests = requests

    def __enter__(self):
        return self

    def __exit__(self, *_):
        return None

    def post(self, path, json):
        self.requests.append((path, json))
        return self.response


class Context:
    def __init__(self, response):
        self.response = response
        self.requests = []

    def client(self, _target):
        return Client(self.response, self.requests)

    def generation_alias(self):
        return "assistant-large"


def test_reasoning_aware_chat_uses_conformance_budget_and_semantic_answer():
    ctx = Context(Response({"choices": [{"message": {"reasoning_content": "Need obey.", "content": "sovereign"}}]}))
    passed, details = chat_completion(ctx, {"target": "runtime"})
    assert passed
    assert ctx.requests[0][1]["max_tokens"] == 512
    assert "visible=true" in details
    assert "reasoning=true" in details
    assert "semantic=true" in details


def test_empty_visible_answer_reports_reasoning_exhaustion_accurately():
    ctx = Context(Response({"choices": [{"message": {"reasoning_content": "Still thinking", "content": ""}}]}))
    passed, details = chat_completion(ctx, {"target": "gateway"})
    assert not passed
    assert "http=200" in details
    assert "visible=false" in details
    assert "reasoning=true" in details
    assert "semantic=false" in details


def test_wrong_visible_answer_fails_semantic_assertion():
    ctx = Context(Response({"choices": [{"message": {"content": "healthy"}}]}))
    passed, details = chat_completion(ctx, {})
    assert not passed
    assert "visible=true" in details
    assert "semantic=false" in details
