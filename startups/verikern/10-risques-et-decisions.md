# Risques et décisions

Sources : Vision (Risks and open questions), Architecture, Design Principles, Delegation Plan.

## Risques

| Risque | Parade | Responsable |
| --- | --- | --- |
| SMILE veut la PI ou l'exclusivité | Partenariat non exclusif ; code et méthode du label restent dans la société | Fondateur, avocat |
| Pas de cofondateur réglementaire au round angel | Lancer la recherche maintenant ; un conseiller solide fait le relais | Fondateur |
| Le label n'a pas de poids seul | Méthode publiée ; endossement par un organisme d'audit accrédité | Cofondateur |
| L'échéance de l'AI Act bouge encore | Vendre la souveraineté et la préparation à l'audit, valables quelle que soit la date ; DORA s'applique déjà | Fondateur |
| Les hyperscalers lancent des offres souveraines UE | Rester neutre vis-à-vis des modèles et des clouds | Fondateur |
| Envoy ne permet pas de chaîner deux processeurs sur une route | Prototype en trois vérifications ; repli sur un second saut Envoy | Fondateur |
| Projet perçu comme dispersé (conseil + plusieurs produits) | Présenter un seul projet ; NEXOPS ONE comme preuve de réalisation, pas comme second projet ; arrêter le conseil avant le seed | Fondateur |
| Conflit entre canaux (SMILE et Village by CA sur une même banque) | Clarifier avant chaque pilote qui mène l'affaire | Fondateur |
| Données confidentielles dans des outils d'IA externes | Règle du plan de délégation : aucune donnée de banque ou de pilote hors des règles de souveraineté | Fondateur |

## Décisions ouvertes

### Société et marque

- [ ] Lieu de direction effective et résidence du gérant (conditionne la structure luxembourgeoise)
- [ ] Fermeture de la SASU française : mise en sommeil ou liquidation amiable
- [ ] Nom du label (Sovgrade en nom de travail)
- [ ] SARL ou SA à la conversion

### Commercial

- [ ] Quelle banque mène le premier pilote, et par quel canal
- [ ] Prix et critères de réussite du pilote payant
- [ ] Part de SMILE

### Équipe et financement

- [ ] Liste restreinte de cofondateurs sur les trois profils
- [ ] Fourchette d'equity du cofondateur
- [ ] Dates de candidature Fit4Start 2027
- [ ] Utilisation des fonds du round angel, jalon par jalon

### Technique

- [ ] Étiquettes de classes de données : en-tête envoyé par l'application (proposé) ou détection dans la passerelle
- [ ] Prestataire d'horodatage qualifié (LuxTrust proposé)
- [ ] Service HSM pour le SaaS
- [ ] Résultat du prototype Envoy

### Façon de travailler

- [ ] Quels outils d'IA pour quels documents, et lesquels sont exclus pour le confidentiel
- [ ] Budget experts du T4 2026 : conseil en marques, notaire, comptable
- [ ] Revue du travail de l'IA : tout avant envoi, ou contrôles ponctuels pour l'interne

## Décisions prises

| Date | Décision |
| --- | --- |
| 20 oct. 2026 | Dix principes de conception ; souveraineté en niveaux, niveau 1 par défaut |
| 20 oct. 2026 | Hachages par défaut, contenu en option ; *fail closed* dans le MVP |
| 20 oct. 2026 | Envoy AI Gateway comme base unique, jamais forké ; Go pour le moteur et l'enregistreur |
| 20 oct. 2026 | Format du journal, vérificateur et méthode du label publiés ; le reste fermé |
| 25 oct. 2026 | Présenter Verikern au Village by CA plutôt que NEXOPS ONE, avec NEXOPS ONE comme preuve de réalisation |
| 27 oct. 2026 | Société : NEXOPS ONE S.à r.l.-S, nouvelle société au Luxembourg ; Verikern, Register Health Check et le label sont des marques de NEXOPS ONE |
| 27 oct. 2026 | Pas de fusion transfrontalière : transfert de l'activité par contrats (cession de PI, nouveaux contrats), puis fermeture de la SASU française |
