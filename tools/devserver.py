"""本地开发服务器：public/ 静态文件 + /api/ 反代到 easyblog-server:8788（模拟线上 nginx）"""
import http.server, os, urllib.request, urllib.error, socketserver

PUB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "public")
UPSTREAM = "http://127.0.0.1:8788"


def proxy(self, method):
    if not self.path.startswith("/api/"):
        self.send_error(404)
        return
    n = int(self.headers.get("Content-Length", 0) or 0)
    body = self.rfile.read(n) if n else None
    headers = {}
    if self.headers.get("Content-Type"):
        headers["Content-Type"] = self.headers["Content-Type"]
    if self.headers.get("Cookie"):
        headers["Cookie"] = self.headers["Cookie"]
    req = urllib.request.Request(UPSTREAM + self.path, data=body, headers=headers, method=method)
    try:
        resp = urllib.request.urlopen(req)
    except urllib.error.HTTPError as e:
        resp = e
    self.send_response(resp.status if hasattr(resp, "status") else resp.code)
    ct = resp.headers.get("Content-Type", "application/json")
    self.send_header("Content-Type", ct)
    for sc in resp.headers.get_all("Set-Cookie") or []:
        self.send_header("Set-Cookie", sc)
    self.end_headers()
    if method == "GET" and self.path.endswith("/health"):
        pass
    try:
        while True:
            chunk = resp.read(256)
            if not chunk:
                break
            self.wfile.write(chunk)
            self.wfile.flush()
    except Exception:
        pass


class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=PUB, **kw)

    def log_message(self, *a):
        pass

    def do_POST(self):
        proxy(self, "POST")

    def do_GET(self):
        if self.path.startswith("/api/"):
            proxy(self, "GET")
        else:
            super().do_GET()

    def do_DELETE(self):
        proxy(self, "DELETE")

    def do_PUT(self):
        proxy(self, "PUT")


socketserver.ThreadingTCPServer.allow_reuse_address = True
socketserver.ThreadingTCPServer(("127.0.0.1", 8643), H).serve_forever()
