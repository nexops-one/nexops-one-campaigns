# Vague 2 — Le coût réel de l'IA en production

Ton : un fondateur qui construit et qui cherche à comprendre, pas un éditeur qui vend. Le module de coût par requête et les économies vérifiées ne sont pas encore implémentés : on parle du problème et de la méthode, jamais d'une fonctionnalité livrée.

Appel à l'action commun : « Écrivez-moi COÛT en message privé » → conversation de 20 minutes, sans démo commerciale. Comptez chaque message dans [suivi.csv](suivi.csv).

---

## V2-P1 · La ligne « IA » de la facture cloud

**FR**

> Votre facture cloud a une ligne « IA ». Savez-vous ce qu'elle paie ?
>
> Dans les équipes que je rencontre, la réponse est souvent : « un peu de tout ». Un assistant interne, deux fonctionnalités client, des tests, un batch de nuit. Tout arrive sur la même ligne, et la finance ne voit qu'un total qui grossit.
>
> Les questions qui restent sans réponse :
> • combien coûte une requête, pour chaque cas d'usage ?
> • combien coûte un client ?
> • quelle équipe a fait doubler la ligne le mois dernier ?
>
> Je travaille précisément sur ce sujet. Si vous faites tourner de l'IA en production et que ces questions vous parlent, écrivez-moi « COÛT » en message : 20 minutes pour comparer nos méthodes, sans démo commerciale.
>
> #FinOps #GenAI #CloudCosts

**EN**

> Your cloud bill has an "AI" line. Do you know what it pays for?
>
> In the teams I meet, the answer is often "a bit of everything". An internal assistant, two customer features, some tests, a nightly batch. It all lands on one line, and finance only sees a total that keeps growing.
>
> The questions that stay unanswered:
> • what does one request cost, per use case?
> • what does one customer cost?
> • which team doubled the line last month?
>
> This is exactly what I'm working on. If you run AI in production and these questions sound familiar, message me "COST": 20 minutes to compare methods, no sales demo.
>
> #FinOps #GenAI #CloudCosts

---

## V2-P2 · Le calcul que peu d'équipes font

**FR**

> Le coût d'une requête d'IA tient en une ligne :
>
> (tokens en entrée × prix d'entrée) + (tokens en sortie × prix de sortie) + la part d'infrastructure (passerelle, vecteurs, GPU si vous hébergez).
>
> Facile. Sauf que pour l'obtenir, il faut relier trois sources qui ne se parlent pas : la facture du fournisseur, les journaux de la passerelle, et l'organisation (quelle équipe, quel produit, quel client).
>
> Exemple fictif : un assistant qui envoie 3 000 tokens de contexte pour répondre en 300. Réduire le contexte de moitié divise presque le coût par deux, sans toucher au modèle. Personne ne le voit tant que le coût n'est pas rattaché au cas d'usage.
>
> Comment faites-vous ce rattachement aujourd'hui : tags, passerelle, tableur ?
>
> #FinOps #LLM #UnitEconomics

**EN**

> The cost of one AI request fits on one line:
>
> (input tokens × input price) + (output tokens × output price) + your share of infrastructure (gateway, vector store, GPUs if you self-host).
>
> Easy. Except that getting it means joining three sources that don't talk to each other: the provider's bill, the gateway logs, and the organisation (which team, which product, which customer).
>
> A made-up example: an assistant that sends 3,000 tokens of context to answer in 300. Halving the context nearly halves the cost, without touching the model. Nobody sees it until the cost is tied to the use case.
>
> How do you make that link today: tags, a gateway, a spreadsheet?
>
> #FinOps #LLM #UnitEconomics

---

## V2-P3 · Les GPU qui dorment

**FR**

> L'endroit le plus cher d'une plateforme d'IA, c'est rarement le modèle. C'est le GPU qui attend.
>
> Un cluster d'inférence dimensionné pour le pic du lundi matin, qui tourne à vide la nuit et le week-end. Un environnement de test qu'on a oublié d'éteindre. Un modèle surdimensionné pour une tâche de classification.
>
> Rien d'illégal, rien de visible dans un tableau de bord de conformité. Juste de l'argent.
>
> La méthode que j'applique : mesurer l'utilisation réelle par heure, la comparer au coût par heure, et classer les actions en euros par an, avec le risque et l'effort à côté. On décide ensuite, en connaissant le prix de chaque option.
>
> Avez-vous déjà mesuré le taux d'utilisation de vos GPU d'inférence ? Écrivez-moi « COÛT » si vous voulez comparer.
>
> #FinOps #GPU #AIInfrastructure

**EN**

> The most expensive part of an AI platform is rarely the model. It's the GPU that's waiting.
>
> An inference cluster sized for the Monday-morning peak, idling at night and over the weekend. A test environment nobody switched off. An oversized model for a classification task.
>
> Nothing illegal, nothing visible on a compliance dashboard. Just money.
>
> The method I use: measure real utilisation per hour, compare it with the cost per hour, and rank the actions in euros per year, with risk and effort next to each one. Then decide, knowing the price of every option.
>
> Have you ever measured the utilisation of your inference GPUs? Message me "COST" if you want to compare.
>
> #FinOps #GPU #AIInfrastructure

---

## V2-P4 · La dépendance en euros

**FR**

> « Nous dépendons trop de ce fournisseur. » Tout le monde le dit. Presque personne ne sait dire combien coûterait de le quitter.
>
> Pourtant les deux questions sont devenues concrètes :
> • DORA demande aux entités financières un plan de sortie pour leurs services TIC critiques ;
> • depuis septembre 2025, le Data Act européen fait du changement de fournisseur cloud une option réelle.
>
> Ce qui manque, c'est un chiffre : la part de la dépense concentrée sur un fournisseur pour les charges critiques, et une estimation du coût de sortie (volume de données, sorties réseau, services à remplacer, prix de l'alternative européenne), avec une fourchette et des hypothèses visibles.
>
> Un score de souveraineté se discute. Un montant en euros, on peut le mettre au budget.
>
> Votre dernier plan de sortie contenait-il un montant ?
>
> #DORA #DataAct #CloudSovereignty

**EN**

> "We depend too much on this provider." Everyone says it. Almost nobody can say what leaving would cost.
>
> Yet both questions have become concrete:
> • DORA requires financial entities to have exit plans for their critical ICT services;
> • since September 2025, the EU Data Act has made switching cloud providers a real option.
>
> What's missing is a number: the share of spend concentrated on one provider for critical workloads, and an exit-cost estimate (data volume, egress, services to replace, price of the European alternative), with a range and visible assumptions.
>
> A sovereignty score can be argued with. A euro amount can go into a budget.
>
> Did your last exit plan include an amount?
>
> #DORA #DataAct #CloudSovereignty

---

## V2-P5 · Économies trouvées, économies vérifiées

**FR**

> « Nous avons identifié 120 000 € d'économies. » Combien ont été réalisées six mois plus tard ?
>
> C'est la question que les directions financières posent, et la plupart des outils FinOps ne savent pas y répondre : ils estiment, ils ne mesurent pas après coup.
>
> La méthode que je défends est simple à énoncer :
> 1. figer la référence au moment où l'action est approuvée ;
> 2. mesurer la dépense réelle après le changement, sur la même période ;
> 3. ne compter comme économie que l'écart vérifié, et le montrer.
>
> C'est plus exigeant. Mais c'est la seule base sur laquelle on peut proposer d'être payé sur résultat.
>
> Chez vous, les économies FinOps sont-elles mesurées après coup ?
>
> #FinOps #CloudCosts #CFO

**EN**

> "We identified €120k in savings." How much was actually realised six months later?
>
> That's the question finance teams ask, and most FinOps tools can't answer it: they estimate, they don't measure afterwards.
>
> The method I stand for is simple to state:
> 1. freeze the baseline when the action is approved;
> 2. measure real spend after the change, over the same period;
> 3. count only the verified difference as a saving, and show it.
>
> It's more demanding. But it's the only basis on which you can offer to be paid on results.
>
> In your organisation, are FinOps savings measured after the fact?
>
> #FinOps #CloudCosts #CFO

*Note : « 120 000 € » est un exemple, pas un résultat client.*

---

## V2-P6 · Ce que m'ont appris les conversations « COÛT »

À rédiger **après** les conversations, avec ce qui a été réellement entendu. Ne pas publier si moins de 3 conversations.

**FR (trame)**

> Il y a quatre semaines, j'ai demandé ici comment vous mesurez le coût de l'IA en production. [N] d'entre vous ont pris 20 minutes pour en parler. Merci.
>
> Ce que j'ai appris :
> 1. [Constat le plus fréquent, avec le nombre : « 4 sur 6 … »]
> 2. [Constat surprenant]
> 3. [Ce qui manque le plus, dans leurs mots]
>
> Ce que je change dans ce que je construis : [une décision concrète].
>
> [Question ouverte pour relancer la discussion.]

**EN (template)**

> Four weeks ago I asked here how you measure the cost of AI in production. [N] of you took 20 minutes to talk about it. Thank you.
>
> What I learned:
> 1. [Most common finding, with the count: "4 out of 6 …"]
> 2. [Surprising finding]
> 3. [What's missing most, in their words]
>
> What I'm changing in what I build: [one concrete decision].
>
> [Open question to keep the discussion going.]
