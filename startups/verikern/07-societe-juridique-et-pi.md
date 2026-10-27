# Société, juridique et propriété intellectuelle

Sources : Vision (Company, team and funding), Task Plan (Company and brand), Design Principles (ouvert ou fermé).

## Structure

- **Aujourd'hui :** l'activité est portée par une SASU française, qui sera fermée.
- **Cible :** une seule société au Luxembourg, **NEXOPS ONE S.à r.l.-S**. Elle détient les produits comme des marques : Register Health Check (DORA), Verikern (passerelle IA) et Sovgrade (label). Pas de holding avant un tour de table.
- **Pourquoi une société neuve plutôt qu'une fusion ou un transfert de siège :** une fusion transfrontalière coûte cher (notaires des deux côtés, certificat du greffe, rapports d'expert, imposition des plus-values latentes en France) et la SASU ne contient rien qu'un contrat ne puisse transférer. Une société neuve, avec une PI proprement cédée, est aussi ce qu'attendent Fit4Start, Luxinnovation et les investisseurs.
- **Capital de la SARL-S :** de 1 € à 12 000 € ; seules des personnes physiques peuvent être associées. Aucun fonds ni business angel passant par sa société ne peut donc entrer sans conversion.
- **Conversion :** en SARL (capital minimum de 12 000 €) ou en SA (30 000 €) avant le round angel. Les VC attendent en général une SA.
- **Conseil :** plus de nouveaux clients au T1 2027 ; les missions en cours se terminent dans la SASU, qui ferme avant le seed. Le jury Fit4Start a écarté un mélange conseil et produit.
- **Nom légal :** « NEXOPS ONE S.à r.l.-S », sans ™. Le ™ reste sur la marque (site, supports) ; ® seulement une fois la marque enregistrée.

**Préalable à tout le reste : le lieu de direction effective.** Si le dirigeant vit et travaille en France, la France peut considérer la société luxembourgeoise comme française et l'imposer en France. Le ministère luxembourgeois attend aussi une direction réellement présente au Luxembourg pour l'autorisation d'établissement. L'affiliation sociale en dépend. Première question au comptable.

### Étapes

| Tâche | Quand | Mode | Statut |
| --- | --- | --- | --- |
| Inventaire de la SASU : contrats, code, marques, domaines, trésorerie, dettes, TVA | Nov. 2026 | Fondateur | À faire |
| Établir à qui appartient le code (président non salarié : probablement le fondateur, sauf cession) | Nov. 2026 | Expert (juriste) | À faire |
| Résidence, direction effective, statut social et fiscal du gérant | Nov. 2026 | Expert (comptable FR et fiduciaire LU) | À faire |
| Autorisation d'établissement (ministère de l'Économie), siège au Luxembourg | Déc. 2026 | Fondateur, House of Entrepreneurship | À faire |
| Statuts de NEXOPS ONE S.à r.l.-S (sans notaire), immatriculation RCS, compte bancaire. Objet : édition de logiciels et services de conformité | Déc. 2026 – janv. 2027 | Fondateur, House of Entrepreneurship | À faire |
| Cession à NEXOPS ONE du code (passerelle, moteur de conformité) et de la méthode du label, au prix de marché documenté si la SASU en est propriétaire | T1 2027 | Expert | À faire |
| Nouveaux contrats signés par NEXOPS ONE ; contrats de conseil en cours terminés dans la SASU ou transférés avec l'accord écrit du client | T1 2027 | Fondateur | À faire |
| Domaines : rediriger les anciens domaines vers nexops.one ; une seule adresse de contact | T1 2027 | Fondateur | À faire |
| Fermeture de la SASU : mise en sommeil (2 ans au plus) ou dissolution et liquidation amiable | Avant le seed | Expert (comptable FR) | À faire |
| Devis du notaire et du comptable pour la conversion en SARL ou SA | Avant le round angel | Expert | À faire |

## Noms et marques

- **Société :** NEXOPS ONE (décidé le 27 oct. 2026). C'est déjà le nom public : domaine nexops.one, landing, campagne, dossier Fit4Start.
- **Produits :** Register Health Check, **Verikern** (passerelle IA), **Sovgrade** (label, nom de travail).

| Tâche | Quand | Statut |
| --- | --- | --- |
| Liste de cinq noms de label | T4 2026 | À faire, Sovgrade en tête de liste |
| Vérification : marques UE et Benelux (TMview, BOIP, classes 9, 42 et 45), registre de commerce, domaines .com, .eu et .lu | T4 2026 | À faire |
| Validation par un conseil en marques | T4 2026 | À faire |
| Dépôt des marques et achat des domaines | T4 2026 | À faire |
| Logo, site web et page LinkedIn | T1 2027 | À faire |

Marques à déposer au nom de NEXOPS ONE : NEXOPS ONE et Verikern, au minimum en Benelux. Mettre à jour `verikern-demo/src/config.ts` (`COMPANY_NAME`, `LABEL_NAME`, `LEGAL_ENTITY`) et, sur Netlify, `LEGAL_ENTITY=NEXOPS ONE S.à r.l.-S`.

## Propriété intellectuelle : ce qui est publié, ce qui est gardé

| Élément | Ouvert / fermé | Pourquoi |
| --- | --- | --- |
| Format du journal et vérificateur | Ouvert | Les auditeurs et les banques doivent vérifier la preuve sans nous faire confiance |
| Méthode du label | Ouvert | Un label n'est crédible que si sa méthode est publique |
| Moteur de politique des niveaux de souveraineté | Fermé | Transforme les règles en décisions à chaque requête |
| Générateur d'evidence pack | Fermé | Le cœur de la conformité produite par le code |
| Outillage de notation et données de calibration | Fermé | La méthode est publique, les données et l'outillage sont à nous |
| Installeurs et opérateurs pour banques | Fermé | Savoir-faire d'intégration |

À faire : une note PI et innovation d'une page, construite sur ce tableau, pour Fit4Start et les aides R&D. Avant de publier quoi que ce soit, vérifier la licence de chaque dépendance (Envoy AI Gateway est sous Apache 2.0).

## Contrats à prévoir

| Contrat | Quand | Points clés |
| --- | --- | --- |
| Accord SMILE | T4 2026 – T1 2027 | Non exclusif, PI et méthode du label à la société, marge de SMILE |
| Pacte de cofondateurs | T1 2027 | Equity, vesting 4 ans avec un an de cliff, rôles |
| Lettre d'intention de pilote | T1 2027 | Non engageante (`verikern-demo/kit/loi-template.md`) |
| Contrat de pilote | T2 2027 | Périmètre, critères de réussite, accord de traitement des données, clauses DORA pour les prestataires TIC |
| Accords partenaires (AI Cordon, Cyvalent, Infratailors) | T2 2027 | |
| Pacte d'associés et term sheet du round angel | T2 2027 | |

## Points juridiques à valider

Les lectures juridiques actuelles (articles 12 et 26 de l'AI Act, DORA, RGPD) sont celles du fondateur. Elles doivent être confirmées par le cofondateur réglementaire ou un juriste avant tout usage commercial.
