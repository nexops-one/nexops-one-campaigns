"""render.py stills t1 t2 ...  -> stills/t.png
render.py video [start end out] -> frames mp4 (no audio), piped to ffmpeg"""
import sys, subprocess, pathlib, time
from playwright.sync_api import sync_playwright

W = pathlib.Path(__file__).parent
FF = str(W.parent.parent / "brag-output/work/node_modules/ffmpeg-static/ffmpeg.exe")
FPS = 30

with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome", args=["--force-color-profile=srgb"])
    page = b.new_page(viewport={"width": 1920, "height": 1080})
    page.goto((W / "index.html").as_uri())
    page.evaluate("window.ready")
    dur = page.evaluate("window.DURATION")
    if sys.argv[1] == "stills":
        (W / "stills").mkdir(exist_ok=True)
        for s in sys.argv[2:]:
            page.evaluate(f"render({float(s)})")
            page.screenshot(path=str(W / "stills" / f"{float(s):05.2f}.png"))
    else:
        start = float(sys.argv[2]) if len(sys.argv) > 2 else 0
        end = float(sys.argv[3]) if len(sys.argv) > 3 else dur
        outp = sys.argv[4] if len(sys.argv) > 4 else "frames.mp4"
        ff = subprocess.Popen([FF, "-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", str(FPS), "-c:v", "mjpeg", "-i", "-",
                               "-c:v", "libx264", "-preset", "medium", "-crf", "16", "-pix_fmt", "yuv420p", "-r", str(FPS), str(W / outp)],
                              stdin=subprocess.PIPE)
        n0, n1 = round(start * FPS), round(end * FPS)
        t0 = time.time()
        for i in range(n0, n1):
            page.evaluate(f"render({i / FPS})")
            ff.stdin.write(page.screenshot(type="jpeg", quality=94))
            if i % 300 == 0:
                print(f"frame {i}/{n1} {time.time() - t0:.0f}s", flush=True)
        ff.stdin.close(); ff.wait()
        print("done", outp)
    b.close()
