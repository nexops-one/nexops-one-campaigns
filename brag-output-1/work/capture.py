"""Render video.html frames with headless Chrome.
usage: python capture.py stills 2.0 6.5 ...   -> work/stills/t_<time>.png
       python capture.py frames               -> work/frames/f_00000.png ... (30 fps)
"""
import sys, os, pathlib
from playwright.sync_api import sync_playwright

W = pathlib.Path(__file__).parent
src = (W / "video.src.html").read_text(encoding="utf-8")
sprite = (W.parent.parent / "landing/assets/brand.svg").read_text(encoding="utf-8")
(W / "video.html").write_text(src.replace("%%SPRITE%%", sprite), encoding="utf-8")

mode = sys.argv[1]
with sync_playwright() as pw:
    b = pw.chromium.launch(channel="chrome")
    pg = b.new_page(viewport={"width": 1920, "height": 1080}, device_scale_factor=1)
    pg.goto((W / "video.html").as_uri())
    pg.evaluate("document.fonts.ready.then(()=>1)")
    pg.wait_for_function("document.fonts.status==='loaded'")
    # touch every weight once so nothing swaps mid-render
    pg.evaluate("render(19.5); render(11); render(5)")
    pg.wait_for_timeout(300)
    if mode == "stills":
        out = W / "stills"; out.mkdir(exist_ok=True)
        for s in sys.argv[2:]:
            pg.evaluate(f"render({float(s)})")
            pg.screenshot(path=str(out / f"t_{float(s):05.2f}.png"))
    else:
        out = W / "frames"; out.mkdir(exist_ok=True)
        dur = pg.evaluate("DURATION"); n = round(dur * 30)
        for i in range(n):
            pg.evaluate(f"render({i/30})")
            pg.screenshot(path=str(out / f"f_{i:05d}.png"))
            if i % 100 == 0: print(i, "/", n, flush=True)
    b.close()
