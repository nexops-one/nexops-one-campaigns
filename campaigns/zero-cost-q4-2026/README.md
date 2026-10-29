# Campagne à coût zéro, T4 2026 – janvier 2027

Objectif : mesurer, sans budget, l'intérêt réel pour trois messages, avant la candidature Fit4Start #18 et avant tout accompagnement de Verikern (Le Village by CA Luxembourg).

Auteur public de tous les contenus : **Nephthali Djabon**, depuis son profil LinkedIn personnel.

## Le fil commun

Une seule voix, un seul thème : **la conformité DORA et AI Act prouvée par vos systèmes, pas par des questionnaires.**

| Vague | Dates | Message testé | Produit derrière | Appel à l'action | Cible |
| --- | --- | --- | --- | --- | --- |
| 1 | 3 nov. → 13 déc. 2026 | Votre registre DORA passera-t-il les contrôles CSSF de 2027 ? | NEXOPS ONE, moteur de registre DORA | Register Health Check gratuit (landing) | Conformité, risques, IT des ManCos, AIFM, PSF, banques, assureurs |
| 2 | 17 nov. → 15 déc. 2026 | Combien coûte réellement votre IA en production ? | NEXOPS ONE, Sovereign Cloud & AI FinOps | Conversation de 20 min (message privé « COÛT ») | CTO, VP Engineering, responsables cloud et FinOps |
| 3 | 1er déc. 2026 → 7 janv. 2027 | Où partent les requêtes d'IA de votre banque, et pouvez-vous le prouver ? | Verikern (non nommé publiquement) | Sovereign AI Readiness Assessment de 45 min (message privé « ASSESSMENT ») | DSI, risques IT, conformité des banques et assureurs |

**Pourquoi cet ordre.** La vague 1 a une échéance réelle (dépôt de mars 2027), une page et une offre prêtes, et elle touche les acheteurs dont Verikern aura besoin ensuite. La vague 2 vérifie le cœur du dossier Fit4Start. La vague 3 prépare Verikern sans exposer un nom qui n'est pas encore déposé.

## Ce qu'on peut dire, et ce qu'on ne dit pas

Repris du *Claims check* de la stratégie Venture Days et des documents Verikern.

| Sujet | On peut dire | On ne dit pas |
| --- | --- | --- |
| Registre DORA | Import du registre existant ; contrôles de complétude et de cohérence ; une donnée manquante est « non évaluée », jamais « conforme » ; auto-hébergé ; cœur open source | « Prêt pour la CSSF », « certifié » ; aucun prix public pour le Filing Season |
| Statistiques | La CSSF a prévenu que les contrôles 2026 portent sur plus de champs ; échéance fin mars, date de référence au 31 décembre 2026 | Le chiffre EY « ~40 % » tant que son année n'est pas confirmée ; « 6,5 % de 947 registres » ; « 86 % des erreurs » |
| Coût de l'IA (vague 2) | Le problème, la méthode, ce que vous construisez, vos questions | Que NEXOPS mesure déjà le coût par requête ou vérifie les économies : ce n'est pas encore implémenté |
| Verikern (vague 3) | La démo existe : chaînage, signatures et vérificateur réels ; modèles et latence simulés | Le nom Verikern ou Sovgrade, un client, une passerelle en production |
| Juridique | « Ma lecture », « à valider avec votre juriste » | Un avis juridique |

## Canaux

| Canal | Usage | Rythme |
| --- | --- | --- |
| LinkedIn, profil personnel | Posts, 2 carrousels PDF, vidéos natives | 3 publications par semaine, du mardi au jeudi, entre 8 h et 9 h (heure de Luxembourg) |
| LinkedIn, commentaires | Commentaires utiles sous les publications de la CSSF, LHoFT, ABBL, ALFI, Luxinnovation et des intervenants des Venture Days | 15 minutes par jour, ouvrés |
| LinkedIn, messages privés | Contacts des Venture Days, personnes qui ont réagi | 5 messages personnalisés par semaine maximum, jamais en masse |
| YouTube | Les 3 vidéos en version longue (16:9) et Shorts (9:16) ; liens UTM en description | 1 vidéo toutes les 2 semaines |
| Facebook / Instagram | Recyclage des Shorts en Reels, sans effort dédié | Le même jour que le Short |

## Mesure

- **Liens :** chaque lien publié porte des balises UTM (voir [calendrier.md](calendrier.md)). Exemple : `?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p01`.
- **Landing :** GoatCounter, sans cookie, activé par la variable Netlify `GOATCOUNTER_CODE`. Les campagnes apparaissent dans l'onglet *Campaigns* de GoatCounter.
- **Réservations :** formulaire Netlify `health-check`, notification par e-mail.
- **Messages « COÛT » et « ASSESSMENT » :** comptés à la main.
- **Tableau :** [suivi.csv](suivi.csv), mis à jour chaque vendredi. Il reprend les statistiques LinkedIn de chaque post (impressions, réactions, commentaires, partages, visites de profil, et le détail des fonctions et entreprises des lecteurs).

## Seuils de décision

Évalués à la fin de chaque vague. « Qualifié » veut dire : une personne du segment cible, dans une entité du segment cible.

| Vague | Intérêt confirmé | À retravailler | Message à abandonner |
| --- | --- | --- | --- |
| 1 | ≥ 5 Health Checks demandés et ≥ 8 conversations qualifiées | 1 à 4 demandes | 0 demande pour plus de 3 000 impressions qualifiées |
| 2 | ≥ 5 conversations qualifiées, dont 2 qui acceptent un second échange | 1 à 4 | 0 pour plus de 3 000 impressions qualifiées |
| 3 | ≥ 3 assessments demandés par des banques ou assureurs | 1 à 2 | 0 pour plus de 2 000 impressions qualifiées |

Les résultats alimentent : la question « Qui paie aujourd'hui ? » du jury Fit4Start, le tableau de bord de [startups/verikern](../../startups/verikern/README.md) et la relance d'Emilie Béchet.

## Règles

1. **Pas d'automatisation** : pas d'outil d'envoi automatique, de scraping ni de « pods » d'engagement (c'est contraire aux conditions de LinkedIn).
2. **Aucun chiffre inventé.** Les exemples chiffrés sont signalés comme fictifs.
3. **Les fichiers de registre** passent par un lien sécurisé, sous NDA, jamais par message LinkedIn ni par le formulaire.
4. **Répondre à chaque commentaire** dans les 2 heures suivant la publication : c'est là que LinkedIn décide de la diffusion.
5. **Un post, une seule demande.** Le lien va dans le post (ou en premier commentaire si la portée baisse : à tester une fois, puis s'y tenir).

## Préparation, avant le 3 novembre

- [ ] Profil LinkedIn : titre, section *Infos*, bannière et section *Sélection* (voir [profil-linkedin.md](profil-linkedin.md)).
- [ ] Netlify : `FOUNDER_NAME=Nephthali Djabon`, `CONTACT_EMAIL`, `CONTACT_PHONE`, `LEGAL_ENTITY`, `GOATCOUNTER_CODE` ; redéployer ; vérifier le compteur dans l'onglet Réseau.
- [ ] Créer le site GoatCounter (gratuit) ; ouvrir `/#toggle-goatcounter` sur vos propres navigateurs pour ne pas compter vos visites.
- [ ] Confirmer l'année du chiffre EY, ou le retirer de la landing et de la vidéo 1.
- [ ] Chaîne YouTube : nom, bannière, description, lien vers la landing.
- [ ] Publier la vitrine de la démo Verikern (avant le 1er décembre) et noter son lien.

## Fichiers

| Fichier | Contenu |
| --- | --- |
| [calendrier.md](calendrier.md) | Les 24 publications, leurs dates et leurs liens UTM |
| [posts-vague-1-registre-dora.md](posts-vague-1-registre-dora.md) | 6 posts, FR et EN |
| [posts-vague-2-cout-ia.md](posts-vague-2-cout-ia.md) | 6 posts, FR et EN |
| [posts-vague-3-preuve-ai-act.md](posts-vague-3-preuve-ai-act.md) | 6 posts, FR et EN |
| [carrousels/](carrousels/) | 2 carrousels PDF (registre DORA, preuve AI Act), FR et EN |
| [videos.md](videos.md) | Scripts des 3 vidéos et état de leur production |
| [messages-directs.md](messages-directs.md) | Messages privés et invitations |
| [profil-linkedin.md](profil-linkedin.md) | Titre, *Infos*, bannière, *Sélection* |
| [suivi.csv](suivi.csv) | Tableau de suivi, une ligne par publication |
