"""Soundtrack for brag.mp4: D major, 96 bpm, polished. Music and effects share one key and one reverb."""
import numpy as np, wave

SR = 48000
DUR = 21.4
N = int(SR * DUR)
rng = np.random.default_rng(7)
t_all = np.arange(N) / SR

def hz(m): return 440 * 2 ** ((m - 69) / 12)
# MIDI notes
D2, A2, B1, G1, A1 = 38, 45, 35, 43, 45
BAR = 2.5  # 96 bpm, 4/4
# bar -> (bass, pad voicing)
CH = {
    "D":  (38, [62, 66, 69, 74]),
    "Bm": (35, [62, 66, 71, 74]),
    "G":  (31, [62, 67, 71, 74]),
    "A":  (33, [61, 64, 69, 73]),
    "Dadd9": (38, [62, 66, 69, 76]),
}
PROG = ["D", "Bm", "G", "A", "Bm", "G", "A", "Dadd9"]

dry = np.zeros((N, 2)); send = np.zeros((N, 2))

def place(buf, sig, start, pan=0.0, gain=1.0):
    i = int(start * SR)
    if i >= N: return
    sig = sig[: N - i] * gain
    l, r = np.cos((pan + 1) * np.pi / 4), np.sin((pan + 1) * np.pi / 4)
    buf[i:i + len(sig), 0] += sig * l * 1.41
    buf[i:i + len(sig), 1] += sig * r * 1.41

def onepole_lp(x, fc):
    a = np.exp(-2 * np.pi * fc / SR); y = np.empty_like(x); z = 0.0
    for i, v in enumerate(x):
        z = (1 - a) * v + a * z; y[i] = z
    return y

def pad_note(f, dur, att=0.6, rel=1.2):
    n = int((dur + rel) * SR); t = np.arange(n) / SR
    s = np.zeros(n)
    for k, amp in [(1, 1), (2, .35), (3, .16), (4, .07)]:
        for det in (-0.12, 0.12):
            s += amp * np.sin(2 * np.pi * f * k * (1 + det / 100 * k) * t + rng.uniform(0, 6.28))
    env = np.minimum(1, t / att) * np.where(t < dur, 1, np.exp(-(t - dur) / (rel / 3)))
    lfo = 1 + 0.08 * np.sin(2 * np.pi * 0.25 * t)
    return s * env * lfo / 6

def pluck(f, decay=0.45, bright=0.3):
    n = int((decay * 5) * SR); t = np.arange(n) / SR
    s = np.sin(2 * np.pi * f * t) + bright * np.sin(2 * np.pi * 2 * f * t) * np.exp(-t / (decay / 3)) \
        + 0.1 * np.sin(2 * np.pi * 3 * f * t) * np.exp(-t / (decay / 5))
    return s * np.exp(-t / decay) * np.minimum(1, t / 0.004)

def sub(f, dur):
    n = int((dur + 0.6) * SR); t = np.arange(n) / SR
    env = np.minimum(1, t / 0.25) * np.where(t < dur, 1, np.exp(-(t - dur) / 0.2))
    return np.sin(2 * np.pi * f * t) * env

def swell(length=0.55, fc=2200):
    n = int((length + 0.5) * SR); t = np.arange(n) / SR
    x = onepole_lp(onepole_lp(rng.standard_normal(n), fc), fc)
    env = np.where(t < length, (t / length) ** 2, np.exp(-(t - length) / 0.08))
    return x * env / (np.abs(x).max() + 1e-9)

# --- music ---
for b, name in enumerate(PROG):
    start = b * BAR; last = b == len(PROG) - 1
    dur = (DUR - start - 1.2) if last else BAR
    bass, voicing = CH[name]
    for j, m in enumerate(voicing):
        place(dry, pad_note(hz(m), dur), start, pan=(-0.5 + j / 3), gain=0.13)
        place(send, pad_note(hz(m), dur), start, pan=(-0.5 + j / 3), gain=0.10)
    place(dry, sub(hz(bass + 12), dur), start, gain=0.16)

# arpeggio: eighth notes from the product scene until the outro chord
EIGHTH = BAR / 8
pattern = [0, 1, 2, 3, 2, 1, 2, 3]
t0 = 8.75
while t0 < 17.45:
    b = int(t0 // BAR); _, v = CH[PROG[b]]
    k = int(round((t0 - b * BAR) / EIGHTH)) % 8
    m = v[pattern[k]] + 12
    accent = 1.0 if k % 2 == 0 else 0.7
    p = pluck(hz(m), decay=0.28, bright=0.25)
    place(dry, p, t0, pan=0.35 if k % 2 else -0.35, gain=0.045 * accent)
    place(send, p, t0, gain=0.05 * accent)
    t0 += EIGHTH

# --- effects (in key, through the same reverb) ---
# hook lines land
for i, m in enumerate([69, 74, 78]):
    p = pluck(hz(m), decay=0.9, bright=0.15)
    place(dry, p, 0.32 + i * 0.25, pan=-0.2 + 0.2 * i, gain=0.07); place(send, p, 0.32 + i * 0.25, gain=0.09)
# dips between scenes
for s in (4.2, 8.25, 13.7, 17.45):
    w = swell(); place(dry, w, s, gain=0.035); place(send, w, s, gain=0.05)
# 40% lands: soft low thump on D + a high chime
n = int(0.8 * SR); tt = np.arange(n) / SR
thump = np.sin(2 * np.pi * (hz(38) * (1 + 0.6 * np.exp(-tt / 0.04))) * tt) * np.exp(-tt / 0.22)
place(dry, thump, 5.88, gain=0.22); place(send, thump, 5.88, gain=0.05)
ch = pluck(hz(86), decay=1.1, bright=0.05); place(dry, ch, 5.9, pan=0.25, gain=0.035); place(send, ch, 5.9, gain=0.06)
# file drops into the report
fd = pluck(hz(57), decay=0.35, bright=0.4); place(dry, fd, 9.92, gain=0.10); place(send, fd, 9.92, gain=0.06)
# findings rows: ascending chord tones
for i, m in enumerate([74, 76, 78, 81]):
    p = pluck(hz(m), decay=0.5, bright=0.2)
    place(dry, p, 10.6 + i * 0.42, pan=-0.3 + 0.2 * i, gain=0.06); place(send, p, 10.6 + i * 0.42, gain=0.07)
# logo lands, then the click
lg = pluck(hz(74), decay=1.6, bright=0.1); place(dry, lg, 18.02, gain=0.08); place(send, lg, 18.02, gain=0.10)
ck = pluck(hz(93), decay=0.05, bright=0.0) * 0.8
click_noise = onepole_lp(rng.standard_normal(int(0.02 * SR)), 5000) * np.exp(-np.arange(int(0.02 * SR)) / (0.003 * SR))
place(dry, ck, 20.1, pan=0.2, gain=0.06); place(dry, click_noise, 20.1, pan=0.2, gain=0.05)
cf = pluck(hz(81), decay=1.2, bright=0.1); place(dry, cf, 20.12, gain=0.05); place(send, cf, 20.12, gain=0.08)

# --- shared reverb (FFT convolution with a dark decaying-noise IR) ---
irn = int(2.2 * SR); it = np.arange(irn) / SR
ir = np.stack([onepole_lp(rng.standard_normal(irn), 3500) * np.exp(-it / 0.55) for _ in range(2)], 1)
ir[: int(0.012 * SR)] = 0
ir /= np.sqrt((ir ** 2).sum(0))
L = 1 << int(np.ceil(np.log2(N + irn)))
wet = np.stack([np.fft.irfft(np.fft.rfft(send[:, c], L) * np.fft.rfft(ir[:, c], L), L)[:N] for c in range(2)], 1)

mix = dry + 0.55 * wet
# gentle bus glue, fade out, normalize to -1 dBFS
mix = np.tanh(mix * 1.4) / 1.4
fade = np.clip((DUR - t_all) / 1.4, 0, 1)[:, None] ** 1.5
fin = np.clip(t_all / 0.03, 0, 1)[:, None]
mix *= fade * fin
mix *= 10 ** (-1 / 20) / np.abs(mix).max()
pcm = (mix * 32767).astype(np.int16)
with wave.open("audio.wav", "wb") as w:
    w.setnchannels(2); w.setsampwidth(2); w.setframerate(SR); w.writeframes(pcm.tobytes())
rms = np.sqrt((mix ** 2).mean()); print("rms dBFS", round(20 * np.log10(rms), 1))
