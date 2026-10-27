# Vidéos

Toutes les vidéos sont produites à coût zéro : HyperFrames pour l'animation, Kokoro en local pour la voix (`af_bella` en anglais, `ff_siwis` en français), musique et bruitages déjà présents dans le projet.

Modifier, contrôler et rendre une vidéo (rendu dans Docker, sans FFmpeg local) : [videos/RENDU-DOCKER.md](videos/RENDU-DOCKER.md).

Sur LinkedIn, publier en vidéo native (pas un lien YouTube) avec sous-titres : la plupart des vidéos sont lues sans le son. Sur YouTube, la description porte le lien UTM de la vidéo (voir [calendrier.md](calendrier.md)).

| ID | Sujet | Durée | Formats | État |
| --- | --- | --- | --- | --- |
| V1-VID1 | Register Health Check | 24,9 s | 16:9, EN et FR | **EN :** [videos/vid1-health-check-en/](videos/vid1-health-check-en/), rendu dans `videos/V1-VID1-health-check-en.mp4`. **FR :** [videos/vid1-health-check-fr/](videos/vid1-health-check-fr/), rendu dans `videos/V1-VID1-health-check-fr.mp4` |
| V2-VID2 | Combien coûte une requête d'IA ? | ~60 s | 16:9 et 9:16 | Script ci-dessous ; à produire avant le 2 décembre |
| V3-VID3 | Falsification détectée | ~45 s | 16:9 et 9:16 | Script ci-dessous ; capture d'écran de la démo Verikern ; à produire avant le 16 décembre |

**Version anglaise :** la scène « stakes » reprend celle de la version française (2025 → 2027 et l'avertissement de la CSSF), et la vo2 a été régénérée en conséquence. Le rendu d'origine `brag-output-2026-10-25-020200/brag.mp4` affiche encore « ~40 % » : ne pas le publier.

---

## V2-VID2 · Combien coûte une requête d'IA ? (~60 s)

Explication animée, sans écran produit : le module de coût par requête n'est pas encore construit, donc on montre une méthode, pas une fonctionnalité. Tous les chiffres sont fictifs et signalés comme tels à l'écran.

| # | Durée | À l'écran | Voix (FR) | Voix (EN) |
| --- | --- | --- | --- | --- |
| 1 | 0–6 s | Une facture cloud, une seule ligne « IA » qui grossit mois après mois | Votre facture cloud a une ligne « IA ». Elle grossit. Mais que paie-t-elle ? | Your cloud bill has an "AI" line. It keeps growing. But what does it pay for? |
| 2 | 6–18 s | La ligne se scinde en quatre usages : assistant interne, deux fonctionnalités client, batch de nuit | Derrière ce total, il y a plusieurs usages, plusieurs équipes, plusieurs clients. Et personne ne sait lequel coûte quoi. | Behind that total are several uses, several teams, several customers. And nobody knows which costs what. |
| 3 | 18–32 s | La formule se construit : tokens en entrée × prix + tokens en sortie × prix + infrastructure | Le coût d'une requête tient en une ligne. Pour le calculer, il faut relier la facture, la passerelle d'IA et l'organisation. | The cost of one request fits on one line. To get it, you have to join the bill, the AI gateway and the organisation. |
| 4 | 32–46 s | Exemple fictif : 3 000 tokens de contexte pour 300 de réponse ; le contexte est réduit de moitié, le coût suit | Exemple fictif : un assistant envoie trois mille tokens de contexte pour répondre en trois cents. Réduisez le contexte de moitié, et le coût presque autant. | A made-up example: an assistant sends three thousand tokens of context to answer in three hundred. Halve the context, and nearly halve the cost. |
| 5 | 46–60 s | Carte de fin : « Écrivez-moi COÛT » + nom | Vous faites tourner de l'IA en production ? Écrivez-moi « coût » : vingt minutes pour comparer nos méthodes. | Running AI in production? Message me "cost": twenty minutes to compare methods. |

Texte du post : V2-P2 (posts-vague-2-cout-ia.md), raccourci.

---

## V3-VID3 · Falsification détectée (~45 s)

Capture d'écran de la démo Verikern (`verikern-demo`, édition hors ligne, Wi-Fi coupé), habillée avec HyperFrames (titres, zooms, sous-titres). Le nom Verikern n'apparaît pas à l'écran : masquer le logo, ou utiliser une édition de démo avec `COMPANY_NAME` neutre dans `src/config.ts`.

| # | Durée | À l'écran | Voix (FR) | Voix (EN) |
| --- | --- | --- | --- | --- |
| 1 | 0–5 s | Titre : « Peut-on modifier un journal d'IA sans être vu ? » | Peut-on modifier un journal d'IA sans être vu ? | Can you change an AI log without being seen? |
| 2 | 5–15 s | Passerelle en direct : scénario 3, une requête vers un modèle américain refusée | Une requête vers un modèle hors UE est refusée. Le refus est journalisé, signé, chaîné. | A request to a non-EU model is refused. The refusal is logged, signed and chained. |
| 3 | 15–30 s | Journal signé : « Modifier et recalculer la chaîne », le vérificateur affiche INVALID | Je modifie l'entrée, puis je recalcule toute la chaîne pour couvrir mes traces. Le vérificateur la rejette. | I change the entry, then recompute the whole chain to cover my tracks. The verifier rejects it. |
| 4 | 30–40 s | Zoom sur la signature ; mention « clé dans le HSM de la banque » | Pour falsifier sans être vu, il faudrait la clé de signature. Elle reste dans le HSM de la banque. | To forge it unseen, you'd need the signing key. It stays in the bank's HSM. |
| 5 | 40–45 s | Carte de fin : « Démo : chaînage, signatures et vérificateur réels ; modèles simulés » + « Écrivez-moi ASSESSMENT » | Votre auditeur peut le vérifier lui-même. Écrivez-moi « assessment ». | Your auditor can check it independently. Message me "assessment". |

Texte du post : V3-P4 (posts-vague-3-preuve-ai-act.md).

---

## Formats verticaux (Shorts, Reels)

Chaque vidéo a aussi une version 9:16 (1080 × 1920) : les mêmes scènes recomposées, sous-titres incrustés, carte de fin plus grande. La publier le même jour que la version 16:9 sur YouTube Shorts, puis en Reel sur Instagram et Facebook, avec le même texte que LinkedIn raccourci à 2 lignes.
