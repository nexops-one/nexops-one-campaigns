"""Original score + SFX for the compliance-engine demo. A minor, 100 BPM. numpy only."""
import numpy as np, wave, pathlib

SR = 44100
DUR = 164.0
N = int(SR * DUR)
rng = np.random.default_rng(7)
BEAT = 0.6
BAR = 4 * BEAT
t_all = np.arange(N) / SR

def hz(m): return 440.0 * 2 ** ((m - 69) / 12)
def env_adsr(n, a, r, sustain=1.0):
    e = np.ones(n) * sustain
    na = max(1, int(a * SR)); nr = max(1, int(r * SR))
    e[:na] = np.linspace(0, sustain, na)
    if nr < n: e[-nr:] *= np.linspace(1, 0, nr)
    return e
def add(buf, start, sig):
    i = int(start * SR)
    if i >= len(buf): return
    j = min(len(buf), i + len(sig)); buf[i:j] += sig[: j - i]
def tone(f, d, harm=(1,), decay=None, phase=0):
    t = np.arange(int(d * SR)) / SR
    s = sum(w * np.sin(2 * np.pi * f * k * t + phase * k) for k, w in harm)
    if decay: s *= np.exp(-t / decay)
    return s
def fft_filter(x, lo=None, hi=None):
    X = np.fft.rfft(x); f = np.fft.rfftfreq(len(x), 1 / SR); g = np.ones_like(f)
    if hi: g *= 1 / np.sqrt(1 + (f / hi) ** 4)
    if lo: g *= 1 / np.sqrt(1 + (lo / np.maximum(f, 1e-3)) ** 4)
    return np.fft.irfft(X * g, len(x))

# ---- section gains (smooth) ----
def curve(points):
    xs, ys = zip(*points); return np.interp(t_all, xs, ys)
drums = curve([(0, 0), (8.9, 0), (9.0, .75), (17.5, .75), (18, 1), (47.5, 1), (48, .45), (55.5, .45), (56, .85),
               (131.4, .85), (131.6, 0), (133.2, 0), (134, .85), (153.5, .85), (154, 0), (DUR, 0)])
arpg = curve([(0, 0), (8.9, 0), (9.2, .5), (18, .9), (48, .9), (56, .7), (153.5, .7), (155, .25), (162, 0), (DUR, 0)])
padg = curve([(0, .0), (1.5, .8), (8.5, 1), (9, .8), (154, .8), (156, 1), (161, .8), (DUR, 0)])
bassg = curve([(0, 0), (8.9, 0), (9, 1), (153.6, 1), (154.4, 0), (DUR, 0)])

# ---- harmony: Am F C G, one bar each ----
CH = [(57, [57, 60, 64]), (53, [53, 57, 60]), (48, [55, 60, 64]), (55, [55, 59, 62])]
nbars = int(np.ceil(DUR / BAR)) + 1

pad = np.zeros(N)
for b in range(nbars):
    root, notes = CH[b % 4]
    st = b * BAR
    for m in notes + [root - 12]:
        for det in (-0.08, 0.0, 0.08):
            s = tone(hz(m + det), BAR + 1.2, harm=((1, 1), (2, .35), (3, .18), (4, .08)), phase=rng.uniform(0, 6))
            add(pad, st, s * env_adsr(len(s), .9, 1.1) * .045)
pad = fft_filter(pad, lo=90, hi=2200) * padg

bass = np.zeros(N)
for b in range(nbars):
    root = CH[b % 4][0] - 24 if CH[b % 4][0] > 50 else CH[b % 4][0] - 12
    for k in range(8):
        st = b * BAR + k * BEAT / 2
        s = tone(hz(root), .28, harm=((1, 1), (2, .3)), decay=.16)
        add(bass, st, s * env_adsr(len(s), .006, .05) * (.30 if k % 2 == 0 else .2))
bass *= bassg

kick = np.zeros(N); hat = np.zeros(N)
kt = np.arange(int(.32 * SR)) / SR
ksig = np.sin(2 * np.pi * (45 * kt + (110 - 45) * .035 * (1 - np.exp(-kt / .035)))) * np.exp(-kt / .12)
hn = rng.standard_normal(int(.05 * SR)); hn = hn - np.convolve(hn, np.ones(6) / 6, 'same'); hsig = hn * np.exp(-np.arange(len(hn)) / SR / .012)
for i in range(int(DUR / BEAT) + 1):
    st = i * BEAT
    add(kick, st, ksig * .42)
    add(hat, st + BEAT / 2, hsig * .05)
kick *= drums; hat *= np.clip(drums - .5, 0, 1) * 2 * curve([(0, 0), (18, 0), (18.1, 1), (DUR, 1)])

arp = np.zeros(N)
PAT = [0, 1, 2, 1, 2, 0, 1, 2]
for b in range(nbars):
    notes = CH[b % 4][1]
    for k in range(8):
        m = notes[PAT[(k + (b // 8)) % 8]] + 12
        s = tone(hz(m), .5, harm=((1, 1), (2, .25), (3, .1)), decay=.14)
        add(arp, b * BAR + k * BEAT / 2, s * env_adsr(len(s), .003, .05) * .07 * (1 if k % 2 == 0 else .7))
arp = fft_filter(arp, hi=3500) * arpg

# hook swell into the reveal
sw = np.zeros(N)
seg = (t_all > 5) & (t_all < 9)
nz = fft_filter(rng.standard_normal(N), lo=300, hi=2500)
sw[seg] = nz[seg] * ((t_all[seg] - 5) / 4) ** 2.5 * .05

# ---- SFX ----
sfx = np.zeros(N)
def click(at, g=1.0):
    s = tone(1760, .05, harm=((1, 1), (2, .2)), decay=.008) * .09
    s[:int(.03 * SR)] += fft_filter(rng.standard_normal(int(.03 * SR)), lo=2000, hi=7000) * np.exp(-np.arange(int(.03 * SR)) / SR / .004) * .06
    add(sfx, at, s * g)
def whoosh(at, d=.7, g=1.0):
    n = int(d * SR); x = fft_filter(rng.standard_normal(n), lo=400, hi=3000)
    e = np.sin(np.pi * np.linspace(0, 1, n)) ** 2
    add(sfx, at - d * .6, x * e * .06 * g)
def tick(at, g=1.0):
    n = int(.02 * SR); x = fft_filter(rng.standard_normal(n), lo=1500, hi=6000) * np.exp(-np.arange(n) / SR / .003)
    add(sfx, at, x * .035 * g)
def thud(at):
    t = np.arange(int(.6 * SR)) / SR
    s = np.sin(2 * np.pi * (40 * t + 60 * .05 * (1 - np.exp(-t / .05)))) * np.exp(-t / .22) * .5
    add(sfx, at, s + fft_filter(rng.standard_normal(len(t)), hi=500) * np.exp(-t / .05) * .15)
def bell(at, f, g=1.0):
    s = tone(f, 2.2, harm=((1, 1), (2.76, .25), (5.4, .08)), decay=.7)
    add(sfx, at, s * env_adsr(len(s), .004, .3) * .11 * g)

# hook: count-up shimmer, coverage reveal, stamp
for i in range(14): tick(.2 + i * .1, .5 + i * .03)
whoosh(2.65, .6, .6)
whoosh(4.0, .5, .4)
thud(6.92)
thud(9.0)  # reveal impact
bell(9.0, hz(69), .5); bell(9.02, hz(76), .35)
for at in (17.8, 47.8, 55.7, 153.9): whoosh(at, .8, .8)
for k in range(1, 6): bell(18 + [1, 7, 13, 18, 23][k - 1], hz([69, 72, 76, 79, 81][k - 1]), .25)  # architecture steps
for i in range(22): tick(48.7 + i / 13, .8)          # typing the command
for i in range(14): tick(51.0 + i * .11, .35)        # output lines
clicks = [59.1, 62.6, 64.8, 79.35, 106.6, 118.8, 130.8]
for c in clicks: click(c)
for a, b in [(113.4, 114.3), (114.5, 116.0), (116.2, 117.8)]:   # typing the evidence form
    for x in np.arange(a, b, .075): tick(x, .45)
for t0 in (61, 66, 80, 84, 92, 108, 113, 120, 124, 128, 132, 140, 143, 148): whoosh(t0 + .15, .35, .25)  # page loads
bell(132.15, hz(81), 1.0); bell(132.3, hz(88), .8); bell(132.45, hz(93), .6)   # ready
bell(154.4, hz(69), .6); bell(154.45, hz(76), .45)

# ---- reverb (FFT convolution) ----
def reverb(x, secs=2.4, wet=.28):
    n = int(secs * SR); ir = rng.standard_normal(n) * np.exp(-np.arange(n) / SR / (secs / 6.9))
    ir = fft_filter(ir, lo=200, hi=6000); ir /= np.sqrt(np.sum(ir ** 2))
    L = 1 << int(np.ceil(np.log2(len(x) + n)))
    y = np.fft.irfft(np.fft.rfft(x, L) * np.fft.rfft(ir, L), L)[: len(x)]
    return x * (1 - wet) + y * wet
music = pad + bass + kick + hat + arp + sw
music = reverb(music, 2.6, .22)
sfx = reverb(sfx, 1.8, .25)
mix = music * .9 + sfx * .8
# fade in/out
mix *= np.clip(t_all / .4, 0, 1) * np.clip((DUR - t_all) / 2.5, 0, 1)
mix = np.tanh(mix * 1.4) / 1.4
mix *= .89 / np.max(np.abs(mix))
# stereo: slight width on reverb tail via a 12 ms offset copy
d = int(.012 * SR)
L = mix; R = np.concatenate([mix[:d], mix[:-d]]) * .2 + mix * .8
st = (np.stack([L, R], 1) * 32767).astype(np.int16)
with wave.open(str(pathlib.Path(__file__).parent / "music.wav"), "wb") as w:
    w.setnchannels(2); w.setsampwidth(2); w.setframerate(SR); w.writeframes(st.tobytes())
print("ok", len(mix) / SR)
