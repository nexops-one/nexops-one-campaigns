# Vague 3 — La preuve AI Act, requête par requête

Le produit derrière est Verikern, **non nommé publiquement** tant que le nom n'est pas vérifié et déposé. On parle de « la passerelle que je construis ».

Appel à l'action commun : « Écrivez-moi ASSESSMENT en message privé » → Sovereign AI Readiness Assessment de 45 minutes (trame : `verikern-demo/kit/assessment-template.md`), rapport remis au prospect. Quand la vitrine de la démo est publiée, ajouter son lien avec `utm_campaign=v3-preuve-ai-act`.

Les lectures juridiques sont présentées comme telles (« ma lecture ») : elles doivent encore être confirmées par un juriste.

---

## V3-P1 · Les trois questions qu'un auditeur posera

**FR**

> Votre banque a un assistant GenAI interne. Un jour, un auditeur posera trois questions sur une réponse donnée par cet assistant :
>
> 1. Quel modèle a répondu ?
> 2. Où ce modèle tournait-il, et sous quelle juridiction ?
> 3. En vertu de quelle règle la requête a-t-elle été autorisée ?
>
> Aujourd'hui, dans la plupart des établissements, la réponse se trouve dans un questionnaire rempli après coup. Pas dans le système.
>
> Je construis une passerelle qui répond à ces trois questions pour chaque requête, avec un journal signé que l'auditeur peut vérifier lui-même.
>
> Si vous préparez l'AI Act dans une banque ou une assurance, écrivez-moi « ASSESSMENT » : 45 minutes ensemble, et vous repartez avec un rapport sur l'état de vos usages d'IA.
>
> #AIAct #AIGovernance #Banking

**EN**

> Your bank has an internal GenAI assistant. One day, an auditor will ask three questions about one of its answers:
>
> 1. Which model answered?
> 2. Where was that model running, and under which jurisdiction?
> 3. Under which rule was the request allowed?
>
> Today, in most institutions, the answer sits in a questionnaire filled in after the fact. Not in the system.
>
> I'm building a gateway that answers those three questions for every request, with a signed log the auditor can verify independently.
>
> If you're preparing for the AI Act at a bank or insurer, message me "ASSESSMENT": 45 minutes together, and you leave with a report on where your AI uses stand.
>
> #AIAct #AIGovernance #Banking

---

## V3-P2 · Six mois de journaux, au minimum

**FR**

> Une obligation de l'AI Act passe souvent inaperçue : selon ma lecture de l'article 26, les déployeurs de systèmes d'IA à haut risque doivent conserver les journaux générés automatiquement pendant au moins six mois.
>
> Le scoring de crédit et la tarification d'assurance vie et santé figurent parmi les usages à haut risque.
>
> Conserver des journaux, d'accord. Mais lesquels ? Un journal utile pour un auditeur doit dire, pour chaque requête : quand, quel système d'IA, quel utilisateur (pseudonymisé), quelle règle, quel modèle, où, et quelle décision.
>
> Et il doit être impossible de le modifier discrètement.
>
> Vos journaux d'IA actuels contiennent-ils la version de la règle appliquée ?
>
> #AIAct #RecordKeeping #RiskManagement

**EN**

> One AI Act duty often goes unnoticed: in my reading of Article 26, deployers of high-risk AI systems must keep the automatically generated logs for at least six months.
>
> Credit scoring and life and health insurance pricing are among the high-risk uses.
>
> Keeping logs, fine. But which ones? A log that helps an auditor has to say, for each request: when, which AI system, which user (pseudonymised), which rule, which model, where, and what decision.
>
> And it must be impossible to change quietly.
>
> Do your current AI logs record the version of the rule that was applied?
>
> #AIAct #RecordKeeping #RiskManagement

---

## V3-P3 · Hacher plutôt que stocker

**FR**

> Pour prouver ce qui a été envoyé à un modèle d'IA, faut-il stocker le prompt ?
>
> Non. Et dans une banque, c'est même souvent la pire idée : données personnelles, secret bancaire, informations confidentielles dans un journal de plus.
>
> Le choix que j'ai fait : par défaut, le journal garde les métadonnées et une empreinte (un hachage) du prompt et de la réponse. Cela suffit pour prouver qu'un contenu donné a bien été envoyé, sans le conserver. Le contenu complet n'est gardé que si la banque le décide pour un système précis, et alors chiffré avec sa propre clé.
>
> Journaliser le minimum qui prouve le maximum.
>
> Vos journaux d'IA contiennent-ils aujourd'hui des données que vous préféreriez ne pas avoir ?
>
> #AIAct #GDPR #PrivacyByDesign

**EN**

> To prove what was sent to an AI model, do you need to store the prompt?
>
> No. And at a bank it's often the worst idea: personal data, banking secrecy and confidential information in one more log.
>
> The choice I made: by default, the log keeps metadata and a fingerprint (a hash) of the prompt and the answer. That's enough to prove a given content was sent, without keeping it. Full content is kept only if the bank decides so for a specific system, and then encrypted with its own key.
>
> Log the least data that proves the most.
>
> Do your AI logs hold data today that you'd rather not have?
>
> #AIAct #GDPR #PrivacyByDesign

---

## V3-P4 · Une preuve que l'on vérifie sans nous faire confiance

À publier avec la vidéo 3 (falsification détectée), en vidéo native.

**FR**

> Dans cette vidéo, je modifie une entrée d'un journal d'IA signé, puis je recalcule toute la chaîne pour couvrir mes traces.
>
> Le vérificateur répond : INVALIDE.
>
> Pour falsifier le journal sans être vu, il faudrait la clé de signature. Elle reste dans le HSM de la banque, pas chez nous.
>
> C'est la propriété qui compte pour un auditeur : il peut vérifier la preuve lui-même, avec un outil open source, sans faire confiance au fournisseur.
>
> (Ce que vous voyez est une démo : le chaînage, les signatures et le vérificateur sont réels ; les modèles sont simulés.)
>
> #AIAct #Audit #Cryptography

**EN**

> In this video, I change one entry in a signed AI log, then recompute the whole chain to cover my tracks.
>
> The verifier says: INVALID.
>
> To forge the log unnoticed, you would need the signing key. It stays in the bank's HSM, not with us.
>
> That's the property an auditor cares about: they can check the evidence themselves, with an open-source tool, without trusting the vendor.
>
> (What you see is a demo: hash chaining, signatures and the verifier are real; the models are simulated.)
>
> #AIAct #Audit #Cryptography

---

## V3-P5 · La souveraineté, un niveau plutôt qu'un slogan

**FR**

> « Nous n'utilisons que de l'IA souveraine. » Vraiment, pour tous les usages ?
>
> Dans la réalité, une banque utilise déjà Azure ou AWS en région européenne, et ne va pas tout arrêter demain. Une règle « UE uniquement » bloquerait la plupart des projets.
>
> Je propose de raisonner en niveaux, fixés pour chaque système d'IA :
> • niveau 1 : opérateur et hébergement européens ;
> • niveau 2 : hébergé en UE par une société non européenne ;
> • niveau 3 : hors UE, sur exception explicite et journalisée.
>
> Une donnée « strictement confidentielle » ne sort pas de l'infrastructure de la banque. Un assistant interne peut accepter le niveau 2. L'important : la règle est écrite, appliquée à chaque requête, et prouvée.
>
> Quel niveau accepteriez-vous pour votre assistant interne ?
>
> #SovereignAI #AIGovernance #CloudSovereignty

**EN**

> "We only use sovereign AI." Really, for every use?
>
> In practice, a bank already uses Azure or AWS in EU regions and won't switch it all off tomorrow. An "EU only" rule would block most projects.
>
> I suggest thinking in levels, set for each AI system:
> • level 1: European operator and European hosting;
> • level 2: hosted in the EU by a non-European company;
> • level 3: outside the EU, on an explicit, logged exception.
>
> "Strictly confidential" data never leaves the bank's infrastructure. An internal assistant may accept level 2. What matters: the rule is written down, enforced on every request, and proven.
>
> Which level would you accept for your internal assistant?
>
> #SovereignAI #AIGovernance #CloudSovereignty

---

## V3-P6 · Le 2 décembre 2027 commence par un assistant interne

**FR**

> Selon le calendrier actuel, l'AI Act attend la conformité des systèmes à haut risque au 2 décembre 2027. Cette date a déjà bougé ; elle peut bouger encore. Le besoin de preuve, lui, ne bougera pas.
>
> Par où commencer quand on est une banque ou un assureur ? Je propose rarement le modèle de scoring en premier. Je propose l'assistant GenAI interne :
> • il se déploie vite ;
> • il n'expose pas les clients ;
> • il construit une preuve et des habitudes que l'on réutilisera pour les modèles à haut risque.
>
> Si vous voulez voir où en sont vos usages d'IA, écrivez-moi « ASSESSMENT » : 45 minutes, un rapport à la clé, aucun engagement.
>
> #AIAct #Banking #Insurance #Luxembourg

**EN**

> On the current timeline, the AI Act expects high-risk systems to comply by 2 December 2027. That date has already moved; it may move again. The need for evidence won't.
>
> Where should a bank or insurer start? I rarely suggest the scoring model first. I suggest the internal GenAI assistant:
> • it deploys fast;
> • it doesn't expose customers;
> • it builds evidence and habits you'll reuse for the high-risk models.
>
> If you want to see where your AI uses stand, message me "ASSESSMENT": 45 minutes, a report at the end, no commitment.
>
> #AIAct #Banking #Insurance #Luxembourg
