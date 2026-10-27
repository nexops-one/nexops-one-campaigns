# Vague 1 — Registre DORA 2027

Lien de la landing, à compléter avec l'URL de production : `<SITE>` (EN : `/`, FR : `/fr/`). Chaque lien reçoit son `utm_content` (voir [calendrier.md](calendrier.md)).

Publier la version FR **ou** EN selon l'audience visée ce jour-là ; l'autre version peut servir de premier commentaire. Par défaut : EN pour les posts qui ciblent les fonds et les banques internationales, FR pour les autres.

---

## V1-P1 · Un registre accepté en 2025 peut être rejeté en 2026

**FR**

> Un registre DORA accepté par la CSSF en 2025 peut être rejeté en 2026.
>
> Ce n'est pas moi qui le dis : la CSSF a prévenu que ses contrôles 2026 portent sur davantage de champs. Le prochain dépôt (date de référence au 31 décembre 2026, échéance fin mars 2027) sera contrôlé au moins aussi strictement.
>
> Ce que je recommande aux entités que je rencontre : ne pas attendre mars. Reprendre maintenant le registre déposé l'an dernier, le passer aux contrôles, et corriger pendant qu'il reste du temps pour relancer les prestataires.
>
> C'est ce que fait notre Register Health Check : vous envoyez le registre de 2026, vous recevez sous 48 heures chaque anomalie, où elle se trouve et comment la corriger. Gratuit pour les 20 premières entités, sous NDA.
>
> 👉 <SITE>/fr/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p01
>
> #DORA #CSSF #Luxembourg #RegTech

**EN**

> A DORA register the CSSF accepted in 2025 can be rejected in 2026.
>
> That's not my claim: the CSSF warned that its 2026 checks cover more data fields. The next filing (reference date 31 December 2026, due end of March 2027) will be checked at least as strictly.
>
> My advice to the entities I meet: don't wait for March. Take last year's register now, run it through the checks, and fix it while there is still time to chase your providers.
>
> That's what our Register Health Check does: send your 2026 register, and within 48 hours you get every finding, where it sits and how to fix it. Free for the first 20 entities, under NDA.
>
> 👉 <SITE>/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p01
>
> #DORA #CSSF #Luxembourg #RegTech

---

## V1-P2 · « Non évalué » n'est pas « conforme »

**FR**

> Un champ vide dans un registre DORA, c'est quoi : un succès ou un échec ?
>
> Dans beaucoup de tableaux de bord, c'est… rien. Le contrôle ne s'applique pas, et le score reste vert. Résultat : 100 % de conformité sur 47 % des données.
>
> Nous avons fait un choix inverse : une donnée manquante est affichée « non évaluée », jamais « conforme ». Le score et la couverture sont toujours montrés ensemble.
>
> C'est moins flatteur. C'est aussi la seule façon de savoir ce que la CSSF va voir.
>
> Votre outil actuel affiche-t-il la couverture, ou seulement le score ?
>
> #DORA #RegisterOfInformation #Conformité

**EN**

> An empty field in a DORA register: is it a pass or a fail?
>
> In many dashboards it's… nothing. The check doesn't apply, and the score stays green. You end up with 100% compliance on 47% of the data.
>
> We made the opposite choice: missing data shows as "not assessed", never as a pass. The score and the coverage are always shown together.
>
> It's less flattering. It's also the only way to know what the CSSF will see.
>
> Does your current tool show coverage, or only the score?
>
> #DORA #RegisterOfInformation #Compliance

*Note : « 100 % sur 47 % » est un exemple illustratif, issu de la démo. Le présenter comme tel si quelqu'un le demande.*

---

## V1-P3 · Les cinq vérifications que je fais en premier

**FR**

> Quand j'ouvre un registre d'information DORA, je commence toujours par les cinq mêmes vérifications :
>
> 1️⃣ L'entité déclarante est-elle complètement identifiée (LEI, nom, pays, type, autorité compétente, date de référence) ?
> 2️⃣ Chaque prestataire TIC a-t-il un code d'identification et son type, une raison sociale, un type de personne et un pays de siège ?
> 3️⃣ La maison mère ultime de chaque prestataire est-elle connue, et est-elle elle-même enregistrée comme prestataire ?
> 4️⃣ Pour chaque service, les pays de fourniture, de stockage et de traitement des données sont-ils renseignés ?
> 5️⃣ Chaque accord a-t-il une date de début et une date de fin ?
>
> Rien de spectaculaire. Mais ce sont des données que l'on obtient des prestataires, et cela prend des semaines. D'où l'intérêt de vérifier en novembre plutôt qu'en mars.
>
> Le Health Check fait ces contrôles, et les suivants, sur votre registre 2026 : <SITE>/fr/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p03
>
> #DORA #CSSF #ICTRisk

**EN**

> When I open a DORA register of information, I always start with the same five checks:
>
> 1️⃣ Is the reporting entity fully identified (LEI, name, country, type, competent authority, reference date)?
> 2️⃣ Does every ICT provider have an identification code and its type, a legal name, a person type and a headquarters country?
> 3️⃣ Is each provider's ultimate parent known, and is it registered as a provider too?
> 4️⃣ For every service, are the countries of provision, storage and processing recorded?
> 5️⃣ Does every arrangement have a start date and an end date?
>
> Nothing spectacular. But this data comes from your providers, and getting it takes weeks. That's why checking in November beats checking in March.
>
> The Health Check runs these checks, and the rest, on your 2026 register: <SITE>/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p03
>
> #DORA #CSSF #ICTRisk

---

## V1-P4 · L'outil DORA qui ne crée pas un nouveau problème DORA

**FR**

> Petit paradoxe que l'on oublie souvent : chaque outil SaaS que vous adoptez pour tenir votre registre DORA… devient un prestataire TIC de plus dans ce même registre.
>
> Avec son contrat à documenter, sa maison mère, ses sous-traitants, la localisation de vos données et son plan de sortie.
>
> C'est pour cela que notre moteur de registre :
> • tourne dans votre propre environnement, vos données restent chez vous ;
> • a un cœur open source, donc une sortie documentée et pas d'enfermement ;
> • fournit sa propre fiche pour votre registre, avec un dossier de due diligence fournisseur.
>
> Combien de prestataires votre outillage de conformité a-t-il ajoutés à votre registre ?
>
> #DORA #ThirdPartyRisk #OpenSource

**EN**

> A paradox that often goes unnoticed: every SaaS tool you adopt to maintain your DORA register… becomes one more ICT third party in that same register.
>
> With its contract to document, its parent company, its subcontractors, your data location and its exit plan.
>
> That's why our register engine:
> • runs in your own environment, so your data stays with you;
> • has an open-source core, so there's a documented exit and no lock-in;
> • ships its own entry for your register, with a vendor due-diligence pack.
>
> How many providers has your compliance tooling added to your register?
>
> #DORA #ThirdPartyRisk #OpenSource

*Note : le dossier de due diligence et la licence sont en revue juridique. Ne pas le remettre comme définitif.*

---

## V1-P5 · Fonctions critiques : là où le registre cesse d'être de la saisie

**FR**

> La partie du registre DORA qui demande le plus de travail n'est pas la liste des prestataires. C'est l'évaluation.
>
> Pour chaque fonction soutenue par des services TIC : une évaluation de criticité datée, un RTO, un RPO et l'impact d'une interruption.
> Pour chaque service évalué : sa substituabilité, la date du dernier audit, la possibilité de le réinternaliser, l'existence d'alternatives.
> Et pour chacun : un plan de sortie.
>
> Ces champs ne se remplissent pas depuis un contrat. Ils demandent une décision de quelqu'un qui connaît l'exploitation.
>
> Question sincère : chez vous, qui porte le plan de sortie d'un service critique, la conformité ou l'IT ?
>
> #DORA #OperationalResilience #ExitStrategy

**EN**

> The hardest part of the DORA register isn't the list of providers. It's the assessment.
>
> For every function supported by ICT services: a dated criticality assessment, an RTO, an RPO and the impact of discontinuation.
> For every assessed service: substitutability, the last audit date, whether it can be brought back in-house, whether alternatives exist.
> And for each one: an exit plan.
>
> None of these fields come from a contract. They need a decision from someone who knows how the service actually runs.
>
> Honest question: in your organisation, who owns the exit plan for a critical service, compliance or IT?
>
> #DORA #OperationalResilience #ExitStrategy

---

## V1-P6 · Filing Season 2027 : un calendrier réaliste

**FR**

> Quatre mois avant l'échéance du registre DORA, voici le calendrier que je propose :
>
> 📅 Décembre : figer le périmètre au 31 décembre 2026 et repasser le registre 2026 aux contrôles.
> 📅 Janvier : relancer les prestataires pour ce qui manque (identifiants, maisons mères, sous-traitants, localisation des données).
> 📅 Février : intégrer, revalider, faire approuver les évaluations de criticité et les plans de sortie.
> 📅 Mars : déposer avec une marge, pour absorber un éventuel rejet.
>
> Si vous voulez savoir dès maintenant où vous en êtes : le Health Check est gratuit, sous NDA, rapport sous 48 heures. Pour la suite, une offre Filing Season 2027 à prix fixe, convenu à l'avance.
>
> <SITE>/fr/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p06
>
> #DORA #CSSF #Luxembourg

**EN**

> Four months before the DORA register deadline, here's the timeline I suggest:
>
> 📅 December: fix the scope at 31 December 2026 and rerun the 2026 register through the checks.
> 📅 January: chase providers for what's missing (identifiers, parent companies, subcontractors, data locations).
> 📅 February: integrate, revalidate, get criticality assessments and exit plans approved.
> 📅 March: file with a margin, so a rejection can still be fixed.
>
> If you want to know where you stand now: the Health Check is free, under NDA, with a report within 48 hours. After that, a Filing Season 2027 offer at a fixed price agreed up front.
>
> <SITE>/?utm_source=linkedin&utm_medium=social&utm_campaign=v1-registre-dora&utm_content=p06
>
> #DORA #CSSF #Luxembourg
