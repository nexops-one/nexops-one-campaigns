# Verikern — présentation d'une page

*Destinataire : Emilie Béchet, Le Village by CA Luxembourg. À envoyer avant l'appel de 20 minutes. Version imprimable : [01-one-pager-village-by-ca.html](01-one-pager-village-by-ca.html).*

---

## Verikern · Passerelle d'IA souveraine pour banques et assureurs

**Prouver, requête par requête, que l'IA d'une banque est conforme à l'AI Act.**

### Le problème

Les banques et assureurs supervisés par la CSSF utilisent déjà l'IA générative, et le scoring de crédit comme la tarification d'assurance sont des usages **à haut risque** au sens de l'AI Act. La preuve de conformité est attendue au **2 décembre 2027**. Aujourd'hui :

- les requêtes partent vers des modèles sous contrôle américain, sans preuve de l'endroit où vont les données ni du modèle qui a répondu ;
- la preuve de conformité est rassemblée à la main, par questionnaire, après coup ;
- auditeurs et CSSF demandent une trace que les outils actuels ne produisent pas pour l'AI Act.

### La solution

Une passerelle placée devant les usages d'IA de la banque :

1. **Elle route** chaque requête vers un modèle approuvé, selon un niveau de souveraineté fixé par système d'IA (modèle européen ou auto-hébergé par défaut).
2. **Elle bloque** ce qui n'est pas autorisé : une donnée *Strictement confidentielle* ne quitte jamais l'infrastructure de la banque.
3. **Elle journalise** chaque requête dans un registre signé et infalsifiable, avec des hachages plutôt que le contenu.
4. **Elle génère** le dossier technique AI Act à partir de ce journal, au lieu de le rédiger.

Elle s'installe sur le Kubernetes ou l'OpenShift de la banque, sans aucun flux sortant vers Verikern, ou en SaaS dans un datacentre luxembourgeois.

### Ce qui nous différencie

- **La conformité produite par le code :** la preuve vient du système en fonctionnement, pas de questionnaires.
- **Vérifiable sans nous faire confiance :** le format du journal et le vérificateur sont publiés ; un auditeur contrôle la preuve lui-même.
- **Souverain et neutre :** la banque garde ses clés (HSM) et ses données, et choisit ses modèles et son cloud.
- **Une seule preuve, plusieurs obligations :** chaque champ du journal sert l'AI Act, le registre des prestataires TIC de DORA et le RGPD.

### Client et modèle économique

- **Acheteur :** DSI ou responsable des risques IT ; validation par le compliance officer.
- **Premier pilote :** un assistant GenAI interne pour le personnel. Rapide à déployer, sans risque côté client, il construit une preuve réutilisable pour les modèles à haut risque.
- **Prix :** abonnement annuel de 25 à 50 k€ par système d'IA couvert, evidence pack et label inclus. Fourchette à valider avec les premiers prospects.

### Où nous en sommes (octobre 2026)

- Architecture et principes de conception rédigés et arbitrés ; base technique choisie (Envoy AI Gateway, Go).
- **Démo cliquable** : moteur de règles, chaînage, signatures et vérificateur réels ; modèles et latence simulés, et signalés comme tels.
- Kit de validation client prêt (guide d'entretien, assessment, lettre d'intention) ; premiers entretiens à venir.
- Le fondateur a déjà conçu **NEXOPS ONE**, un outil de registre DORA : même approche, appliquée ici à l'IA.
- Passage à temps plein au **T1 2027**.

### Feuille de route

| Quand | Étape |
| --- | --- |
| T4 2026 → T1 2027 | Entretiens, lettres d'intention, cofondateur réglementaire, structuration de la société |
| 2027 | Levée business angels de 300 à 500 k€ ; MVP ; premier pilote bancaire |
| Avant le 2 déc. 2027 | Un pilote payant en production, puis seed auprès d'investisseurs RegTech |

### Équipe

**Nephthali Djabon**, CEO et fondateur technique : 20 ans d'expérience en infrastructures sécurisées et régulées. Recherche en cours d'un **cofondateur réglementaire** (AI Act, CSSF, DORA) et d'un conseiller en évaluation ML (SnT ou Uni.lu).

### Ce que nous cherchons auprès du Village by CA

1. **Un programme adapté** à notre stade (pré-amorçage, produit en construction).
2. **Des échanges avec des équipes IT Risk ou Conformité** des entités du groupe au Luxembourg, pour valider le besoin et préparer un pilote.
3. **Des conseils pour la levée** de 2027 et la recherche d'un cofondateur réglementaire.

**Contact :** info@nexopsone.eu · +33619199517 · Démo sur demande, en rendez-vous ou en ligne.
