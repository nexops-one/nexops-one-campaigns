# NEXOPS ONE campaigns

Venture Days 2026 landing page for the Register Health Check, in English (`/`) and French (`/fr/`). Plain static HTML, deployed on Netlify.

```
landing/            source of the site (edit here)
  index.html        English landing page
  fr/index.html     French landing page
  thanks/ fr/merci/ form confirmation pages
  privacy/ fr/confidentialite/   privacy notice
  404.html fr/404.html
  assets/           shared CSS, JS and self-hosted IBM Plex fonts
scripts/build.mjs   copies landing/ to dist/, fills {{TOKENS}}, writes sitemap.xml and robots.txt
netlify.toml        build command, security headers, French 404
docs/               strategy and campaign source documents
campaigns/          social campaigns (zero-cost-q4-2026: posts, carousels, videos, tracking)
startups/           start-up development files (verikern)
```

## Deploy on Netlify

1. Push this folder to a Git repository and create a Netlify site from it. `netlify.toml` sets the build command (`node scripts/build.mjs`) and the publish directory (`dist`).
2. In **Site configuration → Environment variables**, set:

   | Variable | Example |
   | --- | --- |
   | `FOUNDER_NAME` | Jane Doe |
   | `CONTACT_EMAIL` | hello@nexops.one |
   | `CONTACT_PHONE` | +352 621 123 456 |
   | `LEGAL_ENTITY` | NEXOPS ONE S.à r.l. |
   | `LEGAL_ENTITY_FR` (optional) | Defaults to `LEGAL_ENTITY` |
   | `SITE_URL` (optional) | https://nexops.one — defaults to the site's primary Netlify URL |
   | `GOATCOUNTER_CODE` (optional) | nexopsone — the code of a free [GoatCounter](https://www.goatcounter.com) site. Adds cookie-free page counts, including campaign (`utm_*`) links, and switches the privacy notice to mention it. Leave it unset locally |

   A production deploy fails until the first four are set, so placeholders never go live. Deploy previews build with placeholders and are excluded from search engines by `robots.txt`.
3. **Forms → Enable form detection**, then redeploy. Both language versions submit to the same form, `health-check`, and each submission carries a `language` field (`en`/`fr`).
4. **Forms → Form notifications**: add an email notification for `health-check` so each booking reaches you straight away.
5. Add the custom domain. Netlify provisions HTTPS automatically.

## Run locally with Docker

```
cp .env.example .env         # optional: fill in contact details; empty values show placeholders
docker compose up -d         # builds and serves on http://localhost:8300
docker compose down
```

The image builds with the same `scripts/build.mjs` as Netlify and serves `dist/` with nginx (non-root, read-only container). [docker/nginx.conf](docker/nginx.conf) mirrors Netlify's behaviour: the same security headers ([docker/security-headers.conf](docker/security-headers.conf), keep in sync with `netlify.toml`), the English and French 404 pages, and font caching. The booking form's POST is accepted but not stored, so you can click through to the thank-you page. Change the port with `PORT` in `.env`; it is bound to 127.0.0.1 and defaults to 8300 to stay clear of the nexops-one stack (8080–8089).

Contact details are baked in at build time. Compose rebuilds on every `up` (`pull_policy: build`), so after editing `landing/` or `.env` just run `docker compose up -d` again. Opening `landing/*.html` directly in a browser always shows the raw `{{TOKENS}}`; only the built site (Docker, `npm run preview` or Netlify) has them filled.

## Local preview without Docker

```
npm run preview      # builds dist/ with placeholders and serves it on http://localhost:8888
```

Form submissions only work on Netlify; here the request fails and the page shows the contact email instead.

## Before going live

- Have the privacy notice (`privacy/`, `fr/confidentialite/`) reviewed once the legal entity exists.
- Confirm the year of EY's "around 40% had filed as of 16 March" figure before stating it anywhere; the page leaves the year out.
- Have a native French speaker proofread `fr/`.
- Point the QR code on the printed one-pager to `/` (English) or `/fr/`.

## Brag skill with kokoro TTS

1. Launch kokoro fastAPI server [Github web page](https://github.com/remsky/Kokoro-FastAPI )
```bash
# For CPU
 docker run -d -p 8880:8880 -v kokoro-saved-voices:/app/voices ghcr.io/remsky/kokoro-fastapi-cpu:latest
 ```

2. Connect Claude Code and Brag plugin to Kokoro

On **Mac / Linux (Bash/Zsh) :**
```bash
export OPENAI_BASE_URL="http://localhost:8880/v1"
export OPENAI_API_KEY="kokoro-local-no-key-needed"
```

On **Windows (PowerShell) :**
```bash
$env:OPENAI_BASE_URL="http://localhost:8880/v1"
$env:OPENAI_API_KEY="kokoro-local-no-key-needed"
```

3. Choose a voice (Optionnal)
```bash
/brag --voice "af_bella"
```
Or
```bash
"Génère le /brag en utilisant la voix locale af_bella"
```

4. Test
```bash
curl http://localhost:8880/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model": "kokoro", "input": "Kokoro local est prêt pour le brag !", "voice": "af_bella"}' \
  --output test_kokoro.mp3
```

