# Rendre les vidéos avec Docker

FFmpeg n'est pas installé sur ce poste. Le rendu passe donc par Docker : HyperFrames lance un conteneur qui contient Chrome et FFmpeg, et le résultat est identique d'une machine à l'autre. L'application HyperFrames sert à modifier les vidéos ; le rendu final se fait en ligne de commande.

## Les projets

| Dossier | Vidéo rendue |
| --- | --- |
| [vid1-health-check-en/](vid1-health-check-en/) | `V1-VID1-health-check-en.mp4` |
| [vid1-health-check-fr/](vid1-health-check-fr/) | `V1-VID1-health-check-fr.mp4` |

Chaque dossier est un projet HyperFrames complet : `index.html` (la composition principale, avec les voix, la musique et les bruitages), `compositions/` (une scène par fichier) et `assets/`. Le dossier `.hyperframes/` est géré par l'outil : ne pas le modifier à la main.

## Prérequis

- Docker Desktop démarré (`docker info` doit répondre).
- Node.js, pour `npx`.
- Environ 1 Go libre pour l'image de rendu `hyperframes-renderer:0.8.193`. Elle est déjà construite sur ce poste ; vérifier avec `docker images hyperframes-renderer`.

Les projets sont figés sur la version `0.8.193` de la CLI. Garder ce numéro dans toutes les commandes : une autre version reconstruirait une nouvelle image Docker.

## Cycle de travail

### 1. Modifier dans l'application HyperFrames

Ouvrir le dossier du projet dans l'application (téléchargement : https://hyperframes.dev/studio/download). L'application enregistre directement dans les fichiers du projet. Elle ajoute des attributs `data-hf-id` aux balises : c'est normal, les laisser.

Sans l'application, l'aperçu dans le navigateur fonctionne aussi :

```powershell
cd C:\Users\nephty\PROJECTS\nexops-one-campaigns\campaigns\zero-cost-q4-2026\videos\vid1-health-check-fr
npx --yes hyperframes@0.8.193 preview
```

L'aperçu s'ouvre sur http://localhost:3002. `Ctrl+C` pour l'arrêter.

### 2. Contrôler avant le rendu

```powershell
npx --yes hyperframes@0.8.193 check
```

Le contrôle doit finir par `Check passed` sans erreur. Corriger toute erreur ; lire chaque avertissement. Avertissements connus et voulus :

- **EN, à 11,5 s :** `content_overlap` sur « Step 1 » et « Secure upload » : c'est le fondu entre le panneau d'envoi et le rapport.
- **Les deux versions :** notes `container_overflow` (info) : les lents zooms avant dépassent légèrement le cadre.

Pour regarder des instants précis sans tout rendre :

```powershell
npx --yes hyperframes@0.8.193 snapshot --at 0,6,12.5,20
```

Les images et une planche contact sont enregistrées dans `.hyperframes\artifacts\snapshots\`.

### 3. Rendre dans Docker

Depuis le dossier du projet (exemple pour la version française) :

```powershell
cd C:\Users\nephty\PROJECTS\nexops-one-campaigns\campaigns\zero-cost-q4-2026\videos\vid1-health-check-fr
npx --yes hyperframes@0.8.193 render --docker --quality delivery -o "..\V1-VID1-health-check-fr.mp4"
```

Pour la version anglaise, remplacer `fr` par `en` dans le dossier et dans le nom du fichier.

- `--docker` : rend dans le conteneur, donc sans FFmpeg local.
- `--quality delivery` : qualité de publication. Pour un essai rapide : `--quality draft` et un autre nom de fichier, pour ne pas écraser la version finale.
- `-o` : fichier de sortie, ici dans le dossier `videos/`. Sans `-o`, le fichier va dans `renders/` du projet.

Durée : environ 2 à 4 minutes pour 25 s de vidéo. La fin attendue ressemble à :

```
◇  ...\videos\V1-VID1-health-check-fr.mp4
   5.9 MB · rendered in 3m 43s
```

### 4. Vérifier le fichier

Regarder la vidéo en entier, avec le son. Vérifier en particulier la synchronisation des voix avec les coupes de scène et la prononciation des sigles (CSSF, DORA, Health Check).

## Messages sans gravité

| Message | Signification |
| --- | --- |
| `Post-render step 'recordRenderSuccess' failed ... EROFS: read-only file system` | Le conteneur ne peut pas écrire son journal dans le projet. La vidéo est bien produite. |
| `Keep editing ... → https://hyperframes.dev/studio/download?os=linux` | Le lien est pour Linux, car la commande tourne dans le conteneur. Prendre la version Windows sur https://hyperframes.dev/studio/download. |
| `This project pins hyperframes@0.8.193 (latest ...)` | Une version plus récente existe. Ne pas mettre à jour en pleine campagne : la nouvelle version reconstruirait l'image. |
| `3 HyperFrames skills out of date or missing` | Concerne l'assistant Claude Code, pas le rendu. |

## En cas de problème

| Problème | Solution |
| --- | --- |
| `Docker image build timed out` | Le premier rendu construit l'image (environ 1 Go), ce qui peut dépasser 10 minutes. Relancer la même commande : Docker reprend là où il s'est arrêté. |
| `Cannot connect to the Docker daemon` | Démarrer Docker Desktop, attendre qu'il soit prêt, relancer. |
| Polices différentes de l'aperçu | Les polices doivent être dans `assets/fonts/` avec une règle `@font-face`. Une police installée seulement sous Windows n'existe pas dans le conteneur. |
| Le son manque | Chaque `<audio>` doit avoir un `id`, `class="clip"`, `data-start` et `data-duration`. Relancer `check`. |
| Le fichier de sortie est verrouillé | Fermer le lecteur vidéo qui l'ouvre, puis relancer. |

## Changer une phrase de la voix off

Les voix viennent de Kokoro, en local (conteneur `sad_villani`, port 8880). Voix : `af_bella` en anglais, `ff_siwis` en français (vo1, vo2 et vo4 à la vitesse 1.1).

1. Démarrer Kokoro : `docker start sad_villani`.
2. Générer le fichier, depuis le dossier du projet (exemple : vo2 en français) :

   ```powershell
   $body = @{ model = "kokoro"; input = "Votre nouvelle phrase."; voice = "ff_siwis"; speed = 1.1; response_format = "wav" } | ConvertTo-Json
   Invoke-WebRequest -Uri http://localhost:8880/v1/audio/speech -Method Post -ContentType "application/json" -Body $body -OutFile assets/vo2.wav
   ```

3. Réparer l'en-tête WAV. Kokoro écrit des tailles invalides, ce qui fausse la durée lue par les outils. Ce script corrige les tailles et affiche la durée :

   ```powershell
   $f = "assets/vo2.wav"; $b = [IO.File]::ReadAllBytes($f)
   $i = 12; while ($i -lt $b.Length - 8) { if ([Text.Encoding]::ASCII.GetString($b, $i, 4) -eq "data") { break }; $i += 8 + [BitConverter]::ToInt32($b, $i + 4) }
   $data = $b.Length - ($i + 8)
   [BitConverter]::GetBytes([int]($b.Length - 8)).CopyTo($b, 4); [BitConverter]::GetBytes([int]$data).CopyTo($b, $i + 4)
   [IO.File]::WriteAllBytes($f, $b)
   "durée : " + [math]::Round($data / [BitConverter]::ToInt32($b, 28), 2) + " s"
   ```

4. Reporter la durée affichée dans `index.html`, sur la balise `<audio id="vo2" ...>` (`data-duration`). La phrase doit se terminer avant la fin de sa scène ; sinon, la raccourcir ou augmenter `speed`.
5. Écouter dans l'aperçu, puis reprendre à l'étape 2 du cycle (contrôle, puis rendu).

Arrêter Kokoro ensuite : `docker stop sad_villani`.
