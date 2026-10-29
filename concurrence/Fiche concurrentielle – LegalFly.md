# Fiche concurrentielle – LegalFly

Oct 29, 2026 · @secura

## Synthèse

LegalFly est un **concurrent adjacent**, pas un concurrent direct. Le seul recoupement réel porte sur la vague 1 (DORA) : LegalFly passe en revue les contrats TIC, alors que NEXOPS ONE vérifie le registre d'information qui en découle. Sur l'AI FinOps (vague 2), il n'y a aucun recoupement, et il est faible sur la preuve des requêtes d'IA (Verikern, vague 3). Le risque principal tient à ses moyens : 15 M€ levés et une plateforme d'agents qui pourrait s'étendre au registre DORA.

## Profil de LegalFly

Société belge d'IA juridique, financée, déjà connue des assureurs luxembourgeois.

| Élément | Ce qu'on sait |
| --- | --- |
| Positionnement | « Le système d'exploitation juridique des entreprises » : des agents d'IA pour l'intake, la revue de contrats, la rédaction, la due diligence et la recherche juridique ([site](https://www.legalfly.com/)) |
| Cible | Directions juridiques internes et équipes métier ; pas les cabinets d'avocats |
| Couverture | Recherche juridique dans plus de 130 juridictions ; plateforme *Agent Studio* pour des workflows réglementés (juridique, achats, conformité, opérations) |
| Sécurité | Déploiement sur site, anonymisation, cadre aligné sur ISO 27001, SOC 2 Type II et RGPD ([page sécurité](https://www.legalfly.com/security)) |
| DORA | Cas d'usage publié en janvier 2025 : revue des contrats fournisseurs TIC pour un assureur, qui estimait le travail à 5 personnes pendant 2 mois ([article](https://legalfly.com/post/dora-compliance-made-easy-with-legal-ai)) |
| Financement | Seed de plus de 2 M€ (nov. 2023, Redalpine) ; Série A de 15 M€ (juillet 2024, Notion Capital, avec Redalpine et Fortino) ([tech.eu](https://tech.eu/2024/07/15/legalfly-raises-eur15m-series-a-for-ai-legal-platform/)) |
| Luxembourg | Finaliste de l'[ACA Innovation Award 2024](https://www.aca.lu/?p=12387) (association des assureurs) en novembre 2024 |
| Prix | Pas publics : vente sur démo |

## Comparaison produit par produit

Le recoupement se limite au DORA, et encore : les deux outils traitent des étapes différentes.

| Notre produit | Ce que fait LegalFly sur le sujet | Recoupement | Acheteur chez le client |
| --- | --- | --- | --- |
| NEXOPS ONE, registre DORA (vague 1) | Extrait et vérifie les clauses des contrats TIC (article 30) ; ne contrôle pas le registre d'information ni le dépôt CSSF | Partiel | Nous : conformité, risques, IT. Eux : direction juridique, achats |
| NEXOPS ONE, Sovereign Cloud & AI FinOps (vague 2) | Rien | Aucun | Nous : CTO, FinOps. Eux : absents |
| Verikern (vague 3) | Protège ses propres requêtes (anonymisation, sur site) ; aucune preuve des requêtes d'IA de la banque, rien sur l'AI Act | Faible | Nous : DSI, risques IT. Eux : absents |

## Nos avantages face à LegalFly

Notre force, c'est la profondeur sur le registre DORA luxembourgeois et la preuve technique, pas l'étendue juridique.

- **Spécialiste du registre d'information.** Contrôles de complétude et de cohérence entre les onglets, alignés sur l'échéance CSSF de fin mars 2027. LegalFly s'arrête au contrat.
- **Honnêteté du résultat.** Une donnée manquante reste « non évaluée », jamais « conforme ». Un outil généraliste fondé sur un LLM ne peut pas offrir cette garantie.
- **Auto-hébergé et cœur open source.** Le client peut auditer le code, et ses données de registre ne sortent jamais de chez lui. LegalFly propose un déploiement sur site, mais son code est fermé.
- **Preuve, pas promesse (Verikern).** Journal chaîné et signé, avec un vérificateur indépendant. LegalFly montre ses certifications ; nous fournissons une preuve que le client vérifie lui-même.
- **Proximité luxembourgeoise.** Fondateur local, discours CSSF, réseau Venture Days, Village by CA et Fit4Start.
- **Offres qui ne la concernent pas.** L'AI FinOps et la preuve AI Act n'ont pas d'équivalent chez elle.
- **Point d'entrée gratuit et ciblé.** Le Register Health Check se fait sans démo commerciale ni cycle d'achat de plateforme.

## Faiblesses de nos produits au lancement

Aucun des trois produits n'est encore complet pour un usage réglementaire en production. Le registre DORA est le plus proche du but, mais son export n'est pas encore validé par les règles officielles.

| Produit | Faiblesse | Effet face à LegalFly ou au client | Correctif prévu |
| --- | --- | --- | --- |
| Registre DORA | L'export xBRL-CSV (Profil A) est indicatif : taxonomie et règles de validation officielles de l'EBA pas encore chargées | Impossible de dire « prêt pour la CSSF » ; le client doit encore valider lui-même son dépôt | Charger la taxonomie EBA (budget Fit4Start) |
| Registre DORA | Auto-hébergé uniquement : il faut une équipe IT pour l'installer | Les petites ManCos et AIFM sans IT ne peuvent pas l'utiliser ; LegalFly propose un SaaS | Option gérée hébergée au Luxembourg |
| Registre DORA | Ne lit pas les contrats : les données du registre doivent déjà exister | Si les données manquent, c'est l'extraction automatique depuis les contrats qui manque, et c'est le point fort de LegalFly | Import d'extractions contractuelles tierces |
| Registre DORA | Catalogues de règles DORA, RGPD et AI Act sans revue juridique ; prix (Filing Season, abonnement) encore à tester | Pas d'argument juridique opposable ; offre commerciale floue | Cofondateur ou conseiller conformité ; tester les prix pendant les 15 premières conversations |
| Sovereign Cloud & AI FinOps | Mesure du coût par requête et vérification des économies pas encore implémentées | On vend une méthode, pas un outil ; difficile de facturer | Construire le module de mesure après la vague 2 |
| Sovereign Cloud & AI FinOps | Rattachement flou à la promesse DORA et AI Act | Message dispersé : trois produits pour un fondateur seul | Décider à la fin de la vague 2 (seuils de décision) |
| Verikern | Démo seulement : modèles, latence et horodatage qualifié simulés ; aucune passerelle en production | Rien à installer chez une banque avant un pilote | Prototype Envoy, puis pilote en 2027 |
| Verikern | Surcoût cible de moins de 50 ms au 95e centile pas encore mesuré | Une DSI demandera l'impact sur la latence dès le premier rendez-vous | Mesure réelle pendant le pilote |
| Verikern | Briques de confiance pas encore choisies : HSM, horodatage qualifié (LuxTrust), modèle de menaces | La valeur de la preuve dépend de ces briques | Devis LuxTrust, EBRC, Nomios ; modèle de menaces au T4 2026 |
| Verikern | Lecture juridique des champs du journal (articles 12 et 26 de l'AI Act) non validée ; nom pas déposé | La preuve AI Act reste une hypothèse ; impossible de nommer le produit publiquement | Validation par un juriste ; dépôt de marque au T4 2026 |

Côté société, l'écart avec LegalFly vient aussi de l'absence de références clients, de certification ISO 27001 ou SOC 2 et de financement.

## Opportunités

La meilleure opportunité est de nous placer *après* LegalFly dans la chaîne DORA, pas contre elle.

1. **Complémentarité DORA.** LegalFly extrait les clauses des contrats, NEXOPS ONE vérifie que le registre tient face à la CSSF. Un client qui a déjà LegalFly a des données contractuelles propres, mais personne ne contrôle son registre.
2. **Partenariat ou intégration.** Proposer un import des extractions LegalFly dans NEXOPS ONE. Ça nous donne un canal vers leurs clients assureurs, sans concurrence frontale.
3. **Message de différenciation.** « Revoir vos contrats ne suffit pas : votre registre doit passer les contrôles de cohérence. » C'est un argument direct pour la vague 1.
4. **Segments qu'elle ne vise pas.** ManCos, AIFM et PSF luxembourgeois, souvent sans direction juridique interne étoffée, donc hors de son cœur de cible.
5. **AI Act, terrain vide.** Elle ne publie rien sur la preuve AI Act ; Verikern peut occuper ce créneau auprès des banques et assureurs avant elle.
6. **Argument Fit4Start.** Une scale-up à 15 M€ investit déjà dans la conformité DORA par l'IA : ça prouve que le marché paie, et notre angle registre reste libre.

## Menaces et signaux à surveiller

La menace réelle : LegalFly ajoute un agent « registre d'information DORA » sur sa plateforme existante.

| Signal | Où regarder | Ce que ça voudrait dire |
| --- | --- | --- |
| Page ou agent « register of information » | Blog et pages produit LegalFly | Entrée directe sur la vague 1 |
| Contenu sur l'AI Act ou la gouvernance de l'IA | Blog, LinkedIn de Dennis Montegnies (CPO) | Rapprochement de Verikern |
| Nouveau client ou partenaire luxembourgeois (assureur, banque, Big Four) | Communiqués, ACA, LHoFT | Ancrage local plus fort que le nôtre |
| Levée de Série B | Presse tech (tech.eu, Forbes Belgique) | Moyens pour élargir vers la conformité |
| Présence aux événements CSSF, ABBL, ALFI | Agendas des événements | Ciblage des mêmes acheteurs que nous |

Autre risque : le prospect confond « revue de contrats DORA » et « registre DORA », et pense que LegalFly suffit.

## Actions recommandées

Priorité : expliquer la différence entre contrat et registre dès la vague 1, qui démarre le 3 novembre.

- [ ] Ajouter un post (ou une slide du carrousel DORA) « Contrats revus ≠ registre conforme », sans nommer LegalFly
- [ ] Pendant les Health Checks, demander « Quel outil utilisez-vous pour vos contrats TIC ? » et noter les réponses dans suivi.csv
- [ ] Prévoir dans NEXOPS ONE un import générique (CSV ou Excel) des extractions contractuelles, compatible avec un export LegalFly
- [ ] Ajouter LegalFly au tableau des concurrents de startups/verikern/04-go-to-market-et-partenaires.md, catégorie « adjacent »
- [ ] Dossier Fit4Start : citer la Série A de LegalFly comme preuve que le marché paie, et expliquer pourquoi notre angle registre et preuve reste libre
- [ ] Lancer une feuille de route de certification (ISO 27001 d'abord) pour préparer la revue fournisseur des banques
- [ ] Vérifier une fois par mois le blog et le LinkedIn de LegalFly (signaux ci-dessus)

## Sources et limites

Les faits sur LegalFly viennent de ses propres pages et de la presse ; ses chiffres clients n'ont pas été vérifiés de façon indépendante. Ses prix et son nombre de clients ne sont pas publics.

- [LegalFly, page d'accueil](https://www.legalfly.com/)
- [LegalFly, sécurité](https://www.legalfly.com/security)
- [LegalFly, DORA compliance made easy with legal AI](https://legalfly.com/post/dora-compliance-made-easy-with-legal-ai) (janvier 2025)
- [ACA Innovation Award 2024, pitch LegalFly](https://www.aca.lu/?p=12387)
- [tech.eu, Série A de 15 M€](https://tech.eu/2024/07/15/legalfly-raises-eur15m-series-a-for-ai-legal-platform/)
- [tech.eu, seed de plus de 2 M€](https://tech.eu/2023/11/28/ai-co-pilot-legalfly-secures-over-2m-in-seed-funding/)
- Interne : campaigns/zero-cost-q4-2026/README.md, startups/verikern/README.md et 04-go-to-market-et-partenaires.md
