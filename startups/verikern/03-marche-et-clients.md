# Marché et clients

Sources : Vision (Customer and problem), `verikern-demo/src/data/investor.ts`, `verikern-demo/kit/`.

## Client cible

| | |
| --- | --- |
| Segment | Banques et assureurs supervisés par la CSSF (et le Commissariat aux Assurances) au Luxembourg |
| Acheteur | DSI ou responsable des risques IT |
| Validation | Compliance officer |
| Déclencheur | Usages à haut risque de l'AI Act (scoring de crédit, tarification vie et santé), preuve due au 2 décembre 2027 ; DORA en vigueur depuis janvier 2025 |
| Point d'entrée | Assistant GenAI interne pour le personnel : rapide, sans risque client, preuve réutilisable |
| Référence secondaire | Uni.lu IT Innovation, pour le banc de test |

## Le problème, dans les mots du client

- « Nous ne savons pas prouver où partent nos requêtes d'IA ni quel modèle a répondu. »
- « Notre preuve de conformité est un questionnaire rempli après coup. »
- « L'auditeur et la CSSF veulent une trace que nos outils ne produisent pas. »

À confirmer ou corriger en entretien : demander au prospect lequel des trois points le concerne le plus (script prospect, minute 0).

## Dimensionnement bottom-up

Établissements dans le périmètre × systèmes d'IA couverts par établissement × prix par système et par an.

| Paramètre | Valeur | Source |
| --- | --- | --- |
| Banques et assureurs dans le périmètre | *À sourcer* | Listes d'entités supervisées CSSF ; Commissariat aux Assurances |
| Systèmes d'IA couverts par établissement | *Hypothèse à valider en entretien* | Entretiens prospects |
| Prix par système et par an | 25 à 50 k€ | Vision, à tester (`kit/wtp-test.md`) |
| Marché adressable annuel au Luxembourg | *Calculé une fois les chiffres sourcés* | |

Ne présenter aucun chiffre de marché non sourcé. Pour des investisseurs internationaux, préparer un second calcul à l'échelle UE.

## Cibles prioritaires dans l'écosystème Crédit Agricole

Entités du groupe présentes au Luxembourg, à approcher via Le Village by CA. Périmètre exact et interlocuteurs à vérifier.

| Entité | Pourquoi | Interlocuteur visé | Statut |
| --- | --- | --- | --- |
| CA Indosuez Wealth (Europe) | Banque privée, siège au Luxembourg | DSI, risques IT, conformité | À approcher |
| CACEIS (Luxembourg) | Services aux fonds, forte exposition DORA | Risques IT, conformité | À approcher |
| Amundi Luxembourg | Gestion d'actifs | Risques IT, conformité | À approcher |
| Crédit Agricole Life Insurance Europe | Assurance vie : tarification potentiellement à haut risque | Actuariat, conformité | À approcher |

Ces entités peuvent aussi passer par SMILE : vérifier la compatibilité avec l'accord SMILE avant tout pilote direct (voir [04](04-go-to-market-et-partenaires.md)).

## Pipeline prospects

Une ligne par organisation. Étapes : identifié → contacté → entretien → assessment → LOI → pilote payant.

| Organisation | Contact | Rôle | Canal | Étape | Prochaine action | Date |
| --- | --- | --- | --- | --- | --- | --- |
| *(vide)* | | | | | | |

## Outils de validation (dans `verikern-demo/kit/`)

| Outil | Usage |
| --- | --- |
| `interview-guide.md` | Guide d'entretien et grille de notation |
| `assessment-template.md` | Sovereign AI Readiness Assessment de 45 minutes, rapport remis au prospect |
| `wtp-test.md` | Test de volonté de payer (Van Westendorp et question du pilote) |
| `loi-template.md` | Lettre d'intention de pilote, non engageante |
| `case-study-template.md` | Étude de cas, prête pour les vrais chiffres du premier pilote |
| `outreach.md` | Messages d'approche DSI, compliance officer, business angel |

Résultat attendu par rendez-vous, par ordre de préférence : LOI signée → date d'assessment → second rendez-vous avec le décideur → introduction à un pair.
