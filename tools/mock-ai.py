"""本地演示用 mock AI 上游：按关键词返回 canned 回复（仅本地演示，勿部署）"""
import http.server, json, time

POSTS = "《把博客塞进 2 核 2G：性能压榨实录》、《Docker 踩坑记》、《为什么我又开始写博客了》"

def reply(q):
    if any(k in q for k in ("你是谁", "名字", "介绍")):
        return "我是站长养的 AI 导览员。本地演示模式——上线后接上真模型，我就能真的陪你聊了。"
    if any(k in q for k in ("文章", "推荐", "看什么", "手记")):
        return "手边有三篇存货：" + POSTS + "。首推性能压榨那篇，站长写它的时候相当得意。"
    if any(k in q for k in ("百宝", "游戏", "工具", "资源")):
        return "百宝库里《王国之泪》是镇店之宝，工具区 Obsidian 和 Excalidraw 都是我的心头好。"
    if any(k in q for k in ("快", "性能", "速度")):
        return "这个站首页 gzip 后 5.7KB，1M 小水管也秒开。快，是它的尊严。"
    return "收到「" + q[:30] + "」。本地演示模式只会背台词，上线接上真模型就能正经聊了～"

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n))
        q = body["messages"][-1]["content"]
        text = reply(q)
        chunks = [text[i:i+6] for i in range(0, len(text), 6)]
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for c in chunks:
            ev = {"choices": [{"delta": {"content": c}}]}
            self.wfile.write(("data: " + json.dumps(ev, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
            time.sleep(0.08)
        self.wfile.write(b"data: [DONE]\n\n")

http.server.ThreadingHTTPServer(("127.0.0.1", 9913), H).serve_forever()
