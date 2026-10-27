# Produit et technologie

Sources : Vision (section Product), `Verikern Gateway Architecture.md`, `Verikern Gateway Design Principles.md`, `verikern-demo/`.

## Les quatre briques, dans l'ordre

| Ordre | Brique | Ce qu'elle fait | Quand |
| --- | --- | --- | --- |
| 1 | Passerelle (gateway) | Route chaque requête vers un modèle européen approuvé, applique le niveau de souveraineté, écrit un journal signé et infalsifiable | MVP, premier pilote |
| 2 | Evidence pack | Transforme journaux, historique du pipeline et code d'infrastructure en dossier technique AI Act | Avec le pilote |
| 3 | Label Sovgrade | Note chaque système d'IA : résidence des données, provenance du modèle, exposition juridictionnelle, réversibilité, obligations AI Act | Après le pilote ; endossement par un organisme accrédité ensuite |
| 4 | Banc de test | Fait tourner un cas d'usage sur plusieurs modèles avec des benchmarks reproductibles | Après le seed, Uni.lu en référence |

**Argument d'innovation en une ligne** (Fit4Start, aides R&D) : Verikern produit pour chaque requête une preuve AI Act vérifiable cryptographiquement, issue du système en fonctionnement et du pipeline de livraison, là où les outils actuels reposent sur des questionnaires.

## Architecture de la passerelle

Une couche Verikern fine au-dessus d'**Envoy AI Gateway** (renommé Agent Router en v1.2.0, octobre 2026), utilisé tel quel, jamais forké, étendu par son hook de traitement externe.

| Composant | Rôle | Ouvert / fermé |
| --- | --- | --- |
| Frontal Envoy | Point d'entrée compatible OpenAI, TLS/mTLS, jetons, streaming, routage | Base open source |
| Moteur de politique | Décide par requête : autorisé, refusé ou rerouté, selon le niveau et les étiquettes de données | Fermé |
| Dépôt de politiques | Règles versionnées par système d'IA, modifiées par revue, bundles signés | Format fermé, contenu au client |
| Enregistreur de preuves | Écrit et chaîne les entrées, scelle des lots signés ; bloque la requête s'il ne peut pas écrire | Fermé |
| Service de clés | Signature et chiffrement via PKCS#11 ; clés logicielles en démo, HSM du client en pilote | Interface |
| Stockage des preuves | Journal append-only (PostgreSQL ou objet en écriture unique), chez le client | Base open source |
| Coffre de contenu | Prompts et réponses complets, seulement sur option, chiffrés avec la clé du client | Fermé |
| Vérificateur | Contrôle un journal exporté : chaîne, signatures, horodatages | **Ouvert** |
| Console d'admin | Systèmes d'IA, niveaux, politiques, consultation et export | Fermé |

Niveaux de souveraineté : **1** opérateur et hébergement UE (par défaut) · **2** hébergé en UE par une société non européenne · **3** hors UE, sur exception journalisée.

## Décisions prises

1. Souveraineté en niveau par système d'IA, niveau 1 par défaut.
2. Hachages par défaut ; contenu complet en option, chiffré.
3. *Fail closed* dans le MVP : sans règle vérifiable ou sans journal, la requête est bloquée.
4. Envoy AI Gateway comme base unique, étendu, jamais forké.
5. Une interface de clés PKCS#11.
6. Publiés : format du journal, vérificateur, méthode du label. Fermés : moteur, enregistreur, générateur d'evidence pack, console.
7. Go pour le moteur et l'enregistreur.

Objectif de performance : surcoût de la passerelle inférieur à 50 ms au 95e centile, hors modèle ; mesure réelle pendant le pilote.

## Questions techniques ouvertes

| Question | Prochaine étape |
| --- | --- |
| Étiquettes de classes de données | Proposition : l'application envoie l'étiquette de la banque (par ex. Microsoft Purview) dans un en-tête ; absence d'étiquette = niveau par défaut. À confirmer avec la banque pilote |
| Horodatage qualifié | LuxTrust e-Timestamp (qualifié eIDAS, RFC 3161) en premier choix : demander un devis, tester dans le prototype |
| HSM pour le SaaS | Poser à LuxTrust, EBRC et Nomios les quatre questions : service HSM géré ? certifié FIPS 140 niveau 3 ou Common Criteria ? hébergé au Luxembourg ? accès PKCS#11 ? |
| Chaînage des processeurs Envoy | Prototype en trois vérifications : même route que le processeur natif, politique avant routage, hachage du streaming sans délai notable. Repli : un second saut Envoy |
| Lecture juridique des champs du journal | À valider par le cofondateur réglementaire ou un juriste (articles 12 et 26 de l'AI Act, DORA, RGPD) |

## La démo (`verikern-demo/`)

Maquette cliquable en React/Vite, bilingue FR/EN, en deux éditions : **hors ligne** (un seul fichier HTML, Wi-Fi coupé en rendez-vous bancaire) et **vitrine** (GitHub Pages ou Netlify, sans collecte de réponses).

| Réel, calculé dans le navigateur | Simulé, et signalé à l'écran |
| --- | --- |
| Moteur de règles, chaînage, signatures, vérificateur, détection de falsification, export | Modèles, latence, horodatage qualifié |

- Scripts de démo : investisseur en 7 minutes, prospect en 12 minutes (`DEMO_SCRIPT.md`).
- Avant chaque publication : `npm test` doit afficher `13 passed`, et `src/config.ts` doit être à jour.
- Lien de la vitrine publique : *à renseigner une fois publiée*.

## Prochaines tâches produit

| Tâche | Quand | Statut |
| --- | --- | --- |
| Modèle de menaces (threat model) | T4 2026 | À faire |
| Prototype Envoy : les trois vérifications | T4 2026 – T1 2027 | À faire |
| Proxy de routage, niveaux de souveraineté, journal signé | T1 2027 | À faire |
| Environnement de démo technique pour SMILE et les investisseurs | T1 2027 | À faire |
| SSO, rôles, packaging Helm/OpenShift hors ligne | T2 2027 | Après recrutement de l'ingénieur |
| Durcissement et test d'intrusion externe | T3 2027 | À faire |
| Déploiement pilote, puis production avant le 2 déc. 2027 | T3 – T4 2027 | À faire |
