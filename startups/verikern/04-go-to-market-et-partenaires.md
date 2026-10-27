# Go-to-market et partenaires

Sources : Vision (Delivery and pricing, Partners and competitors), Task Plan (Sales and partners).

## Canaux

| Canal | Rôle | Statut |
| --- | --- | --- |
| SMILE (intégrateur IA pour acheteurs régulés) | Intégrateur et revendeur **non exclusif** ; c'est SMILE qui a demandé la passerelle | Accord à négocier (T4 2026 – T1 2027) |
| Le Village by CA Luxembourg | Accès aux entités du groupe Crédit Agricole et à leurs investisseurs | Premier contact établi (voir [08](08-ecosysteme-et-accompagnement.md)) |
| Vente directe | Entretiens DSI et compliance avec le kit de validation | À démarrer |
| LHoFT | Programme fintech, introductions auprès d'institutions | À contacter |

**Règle SMILE :** l'accord garde la PI et la méthode du label dans la société et fixe la marge de SMILE. Pour une banque arrivée par un autre canal (Village by CA par exemple), clarifier avant tout pilote qui mène l'affaire et si SMILE intervient comme intégrateur.

## Offre et prix

| Mode | Pour qui | Comment |
| --- | --- | --- |
| Sur site | Banques qui gardent tout en interne | Kubernetes ou OpenShift du client, Helm ou opérateur, installation hors ligne, mises à jour tirées par le client |
| SaaS Luxembourg | Assureurs et établissements plus petits | Cluster Verikern dans un datacentre luxembourgeois (LuxConnect ou fournisseur local), un namespace isolé par client |

**Prix :** abonnement annuel de 25 à 50 k€ par système d'IA couvert, evidence pack et label inclus. C'est une estimation : la tester avec SMILE et la première banque, puis fixer le prix du pilote.

À décider : prix et durée du pilote payant, critères de réussite, part de SMILE.

## Partenaires

| Société | Activité | Relation | Statut |
| --- | --- | --- | --- |
| SMILE | Usine d'IA agentique, intégrateur | Intégrateur et revendeur non exclusif | À signer |
| Uni.lu IT Innovation | Équipe IA de 4 personnes | Client de référence du banc de test | À formaliser |
| AI Cordon | Protection contre l'injection de prompt (lauréat Fit4Start) | Se branche sur la passerelle | À signer (T2 2027) |
| Cyvalent | Gouvernance NIS2, DORA, CRA, ISO 27001 (lauréat Fit4Start) | Couvre les réglementations hors IA | À signer (T2 2027) |
| Infratailors | Planification de déploiements souverains (spin-off SnT) | Couche infrastructure | À signer (T2 2027) |
| LuxTrust | Horodatage qualifié eIDAS, potentiellement HSM | Fournisseur | Devis à demander |

Aucun partenariat n'est signé à ce jour.

## Concurrence

Aucun lauréat Fit4Start ne couvre la preuve de conformité AI Act. Les concurrents sont les outils américains et britanniques de gouvernance de l'IA (politiques, inventaire de modèles), surtout hébergés aux États-Unis ; peu proposent un hébergement souverain.

| Objection | Réponse |
| --- | --- |
| « Pourquoi pas Azure OpenAI en région UE ? » | C'est le niveau 2, autorisé si la politique le permet. Verikern le rend explicite et le prouve à chaque requête |
| « Une banque peut le faire avec Envoy ou LiteLLM. » | Ils transportent le trafic sans produire de preuve. La valeur est la correspondance champ → obligation et l'evidence pack généré |
| « Les hyperscalers lancent des offres souveraines. » | Verikern reste neutre vis-à-vis des modèles et des clouds et tourne là où le client tourne déjà |

## Tâches commerciales

| Tâche | Quand | Statut |
| --- | --- | --- |
| Offre d'une page pour SMILE | T4 2026 | À faire |
| Accord non exclusif avec SMILE | T4 2026 – T1 2027 | À faire |
| Liste de 3 à 5 banques ou assureurs ayant un projet d'assistant GenAI interne | T1 2027 | À faire |
| Entretiens de découverte et test du prix | T1 2027 | À faire |
| LOI de la banque pilote, à montrer aux business angels | T1 2027 | À faire |
| Contrat de pilote payant : périmètre, critères, accord de traitement des données, clauses DORA | T2 2027 | À faire |
| Revue fournisseur de la banque (circulaires CSSF sur l'externalisation, DORA) | T2 – T3 2027 | À faire |
| Étude de cas et deuxième contrat pour le seed | T4 2027 | À faire |
