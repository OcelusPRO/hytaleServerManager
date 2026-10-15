# Hytale Server Manager

Image Docker légère pour héberger un serveur dédié Hytale, pilotée entièrement
depuis une interface web protégée par mot de passe (port 8080).

- **Image minimale (~140 Mo)** : seul le gestionnaire (binaire Go statique,
  interface React incluse) est embarqué. Tout le reste est téléchargé
  automatiquement dans le volume `/data`.
- **Une seule action demandée** : se connecter avec son compte Hytale. Un bouton
  ouvre une fenêtre directement sur la page d'autorisation Hytale (code
  pré-rempli) ; elle se ferme toute seule une fois la connexion validée.
- **Installation automatique** :
  1. au démarrage du conteneur : Java 25 (Eclipse Temurin, SHA-256 vérifié) ;
  2. dès que le compte est connecté : les fichiers du serveur, téléchargés via le
     service d'assets Hytale (même mécanisme que l'updater intégré au serveur,
     SHA-256 vérifié) ;
  3. ensuite : vérification des mises à jour toutes les heures, installées
     automatiquement quand le serveur est arrêté (ou sur demande, avec arrêt et
     redémarrage automatiques).
- **Authentification Hytale** : *device code flow* OAuth (méthode B du
  *Server Provider Authentication Guide*), refresh token chiffré et renouvelé
  chaque jour, session de jeu créée à chaque démarrage et transmise au serveur
  par variables d'environnement (méthode C, `HYTALE_SERVER_SESSION_TOKEN` /
  `HYTALE_SERVER_IDENTITY_TOKEN` + `--owner-uuid`).
- **Mondes** : plusieurs mondes, chacun avec son dossier serveur (`universe/`,
  `mods/`, `config.json`, whitelist, bans, permissions). Un premier monde est
  créé automatiquement.
- **Console avec autocomplétion** : commandes, sous-commandes et arguments
  (y compris ceux des mods) appris auprès du serveur après chaque démarrage via
  `/commands dump` et `/help`, sans polluer la console ; mis en cache par monde
  et reconstruits seulement quand la version du jeu ou les mods changent.
- **CurseForge** (optionnel) : renseignez une clé API dans **Paramètres** pour
  rechercher des mods et des modpacks Hytale, les installer en un clic
  (dépendances requises incluses, choix de la version), et mettre à jour les mods
  installés depuis CurseForge. Les identifiants de Hytale sont découverts
  automatiquement via l'API ; la clé est stockée chiffrée. Les mods dont l'auteur
  interdit les téléchargements tiers doivent être ajoutés à la main (envoi de
  fichier). Les modpacks au format `manifest.json` CurseForge sont pris en charge
  (mods + dossier `overrides`).
- **Mods par monde**, **sauvegardes** (création, téléchargement, restauration,
  import), **console temps réel**, **joueurs** (expulsion, ban, op, whitelist),
  **cycle de vie** (démarrage auto, redémarrage après plantage, arrêt propre).
- **Ressources** : graphiques de la mémoire et du CPU du processus Java (avec la
  limite `-Xmx`) et du conteneur entier (gestionnaire compris, lu dans son
  cgroup comme `docker stats`), des joueurs connectés et de l'état du serveur
  (en marche, en veille, arrêté) sur 1 h à 14 jours, avec la consommation
  moyenne du conteneur dans chaque état. Mesures toutes les 5 s, agrégées par
  minute et conservées 14 jours dans `/data/manager/metrics.json`.
- **Métriques en direct (optionnel)** : le plugin
  [MetricsStream](internal/metricsstream) (TPS/MSPT par monde, JVM, joueurs,
  événements de jeu : chat, blocs, morts, dégâts…) est **désactivé par défaut**.
  Une fois l'option « Plugin de métriques en direct » activée dans les
  réglages, le gestionnaire copie le plugin dans le dossier `mods/` du monde
  **au prochain démarrage** du serveur (et le retire si l'option est coupée).
  Le plugin envoie ses données en HMSP (protocole binaire UDP, voir
  [`internal/hmsp`](internal/hmsp)) vers `127.0.0.1:9877` ; le gestionnaire
  vérifie le HMAC (secret généré dans `/data/manager/metrics-stream.key`),
  réassemble les fragments, décompresse et affiche le résultat sur le tableau
  de bord. Le port se change avec `HSM_METRICS_STREAM_PORT`.
- **Passkeys** : connexion au panneau avec l'empreinte, le visage ou le code
  d'un appareil (WebAuthn, vérifié par la bibliothèque
  [go-webauthn](https://github.com/go-webauthn/webauthn)). Une fois une passkey
  enregistrée, la connexion par mot de passe peut être désactivée.
  `HSM_RESET_PASSWORD=true` régénère le mot de passe **et** supprime les
  passkeys (recours en cas de perte). Les passkeys exigent HTTPS ou
  `http://localhost`.
- **Langues** : interface en anglais par défaut, en français si le navigateur
  est en français ; le choix se change dans **Paramètres** et s'applique à
  l'enregistrement. Le serveur s'exprime en anglais et l'interface traduit ses
  messages (erreurs, console). La langue des annonces en jeu (comptes à rebours
  des mises à jour) se règle dans **Paramètres**.
- **Diagnostics de l'hôte** : le panneau signale un tampon UDP du système trop
  petit (`net.core.rmem_max`) avec les commandes à lancer sur l'hôte. Le
  conteneur fournit au serveur un identifiant machine stable
  (`/data/machine-id`, lié à `/etc/machine-id`).

## Démarrage rapide

```bash
docker compose up -d --build
```

```bash
docker logs hytale
```

Le mot de passe de l'interface est généré au premier lancement et affiché dans
les logs (aussi stocké dans `/data/manager/initial-password.txt` jusqu'à ce que
vous le changiez dans **Paramètres**).

Ouvrez `http://<hôte>:8080`, connectez-vous, puis cliquez sur
**Se connecter avec Hytale** : c'est tout. Le serveur se télécharge, puis le
bouton **Démarrer** du tableau de bord devient disponible.

Les joueurs se connectent en **UDP 5520** (QUIC) : ouvrez/redirigez ce port en UDP.

## Configuration

| Variable              | Défaut  | Rôle |
|-----------------------|---------|------|
| `HSM_LISTEN_ADDR`     | `:8080` | Adresse de l'interface web |
| `HSM_DATA_DIR`        | `/data` | Racine des données persistantes |
| `HSM_SECURE_COOKIES`  | `false` | Cookie `Secure` (à activer derrière HTTPS) |
| `HSM_TRUST_PROXY`     | `false` | Utiliser `X-Forwarded-For` pour l'anti-bruteforce |
| `HSM_MAX_UPLOAD_MB`   | `1024`  | Taille max d'un mod ou d'une archive importée |
| `HSM_METRICS_STREAM_PORT` | `9877` | Port UDP local du flux du plugin de métriques (optionnel) |
| `HSM_RESET_PASSWORD`  | `false` | Régénère le mot de passe et supprime les passkeys au démarrage (à retirer ensuite) |
| `HSM_BINDHOOK_LIB`    | `/usr/local/lib/hsm-bindhook.so` | Bibliothèque d'interception du `bind()` (mise en veille) |
| `HSM_SLEEP_TEST_WAKE_DELAY` | — | Test uniquement : retarde le démarrage après un réveil (ex. `90s`) pour mesurer la patience des clients |

Mémoire Java, port de jeu, mode `offline`, cache AOT, Sentry, sauvegardes natives,
démarrage/redémarrage automatique se règlent dans **Paramètres**.

### Mise en veille (désactivée par défaut)

Activez **Mise en veille** dans **Paramètres** pour économiser la mémoire et le
CPU quand personne ne joue. Une fois le serveur vide depuis le délai choisi
(10 min par défaut), il est arrêté proprement (monde sauvegardé) et le
gestionnaire écoute à sa place sur le port UDP du jeu. Dès qu'un joueur tente de
se connecter, le serveur redémarre et le joueur entre en jeu **dès sa première
tentative**, sans proxy entre les joueurs et le serveur (≈ 10 s d'attente avec
un démarrage de 8 s).

Comment : le client Hytale n'envoie qu'un paquet par tentative (une tentative
toutes les 20 s, deux au total) et ne le retransmet jamais. Le gestionnaire
laisse donc ces paquets non lus dans la file de son socket. Une petite
bibliothèque `LD_PRELOAD` (`native/bindhook`, chargée seulement quand la veille
est activée) intercepte le `bind()` du serveur : le gestionnaire lui transmet
alors son socket, paquets en attente compris, qui remplace celui du serveur
(options recopiées). Le serveur ne commence à le lire qu'une fois démarré
(« Hytale Server Booted! ») : avant, il refuserait les joueurs. Il reçoit
ainsi la connexion du joueur comme s'il venait de l'accepter lui-même.

- Le serveur dispose d'environ 40 s pour démarrer (au-delà, le client
  abandonne : il suffit de réessayer).
- Replis : si le socket ne peut pas être transmis, le port est simplement
  libéré au moment du bind (le premier essai échoue alors, le second, 20 s
  plus tard, passe) ; si le serveur ne parvient pas à prendre le port, il est
  redémarré sans maintien du port ; hors de l'image Docker (Windows,
  bibliothèque absente), le port est libéré juste avant le démarrage.
- Les tentatives reçues pendant la veille et chaque étape de la remise du port
  sont journalisées dans la console.
- Joueurs IPv6 : sous Linux, la sentinelle n'écoute qu'en IPv4 (Docker
  transmet le trafic IPv6 publié en IPv4).
- Seuls les paquets d'ouverture de connexion QUIC réveillent le serveur (les
  scans et paquets isolés sont ignorés).
- Un arrêt manuel coupe la veille : le serveur reste arrêté. **Réveiller** (ou
  **Démarrer**) le relance immédiatement.
- Si une mise à jour automatique est en attente, elle passe avant la veille.

## Sécurité

- Conteneur non-root (uid 1000), système de fichiers en lecture seule, toutes
  les capabilities retirées, `no-new-privileges` (voir `docker-compose.yml`).
- Mot de passe : 128 bits d'entropie, stocké en PBKDF2-SHA256 (600 000 itérations).
- Sessions : jeton aléatoire opaque, cookie `HttpOnly` + `SameSite=Strict`,
  expiration après 12 h d'inactivité (7 jours max), révocation globale au
  changement de mot de passe. Verrouillage 15 min après 5 échecs de connexion.
- CSRF : en-tête obligatoire + contrôle `Origin`/`Sec-Fetch-Site` sur toute requête modifiante.
- En-têtes stricts (CSP sans inline, `X-Frame-Options`, `nosniff`…). L'interface
  n'insère jamais de données en HTML (`textContent` uniquement).
- Jetons Hytale chiffrés au repos (AES-256-GCM), transmis au serveur par
  l'environnement (jamais visibles dans la liste des processus).
- Archives extraites sans zip-slip ni liens symboliques, avec limites de taille ;
  JRE vérifié par SHA-256 ; noms de mods et commandes console strictement validés.
- Pour une exposition sur Internet, placez l'interface derrière un reverse proxy
  HTTPS (`127.0.0.1:8080:8080` + `HSM_SECURE_COOKIES=true`).

## Données (`/data`)

```
manager/    secrets : hash du mot de passe, clé de chiffrement, jetons Hytale, réglages
runtime/    Java
game/       Server/HytaleServer.jar, Assets.zip (remplacés atomiquement à chaque mise à jour)
saves/<id>/ un dossier serveur par monde (répertoire de travail du serveur)
backups/    archives zip par monde
```

## Limites connues

- La liste des joueurs connectés est déduite des messages de connexion de la
  console (format non documenté, motifs dans `internal/players/tracker.go`).
- Les actions joueurs envoient `/kick`, `/ban`, `/unban`, `/op add|remove`,
  `/whitelist add|remove` (modèles dans `internal/players/commands.go`).
- L'auto-update intégré du serveur est désactivé (`HYTALE_DISABLE_UPDATES`) :
  le gestionnaire s'en charge.
- Le téléchargement des fichiers du jeu utilise l'endpoint
  `account-data.hytale.com/game-assets/` employé par l'updater du serveur ; il
  n'est pas documenté publiquement et pourrait évoluer.

## Développement

Backend (Go 1.26 ; seule dépendance externe : go-webauthn, pour les passkeys) :

```bash
go test ./...
```

Interface (React 19 + TypeScript + Tailwind CSS v4, dans `web/`) :

```bash
npm --prefix web install
```

```bash
npm --prefix web run dev
```

Le serveur Vite proxifie `/api` vers `http://localhost:8080`. `npm --prefix web run build`
produit `web/dist`, embarqué dans le binaire Go (`web/embed.go`) : construisez
l'interface avant `go build`. Le Dockerfile enchaîne les deux étapes.

Architecture (`internal/`) :

| Paquet | Responsabilité |
|---|---|
| `config`, `paths` | Variables d'environnement, emplacement des données |
| `auth` | Mot de passe, sessions, anti-bruteforce du panneau |
| `hytaleapi` | Client HTTP Hytale : OAuth, profils, sessions de jeu, assets |
| `hytaleauth` | Device flow, stockage chiffré et rotation des jetons, sessions de jeu |
| `install` | Java, fichiers du jeu et `Provisioner` (installation/mises à jour automatiques) |
| `gameserver` | Processus serveur, console, construction de la ligne de commande |
| `lifecycle` | Orchestration démarrage/arrêt/redémarrage et prérequis |
| `sleep` | Mise en veille du serveur vide et réveil à la connexion |
| `metrics`, `hostcheck` | Historique des ressources, avertissements liés à l'hôte |
| `passkey` | Passkeys (WebAuthn) du panneau |
| `saves` | Mondes, mods par monde, sauvegardes |
| `players` | Commandes de modération, fichiers de listes, joueurs connectés |
| `settings` | Réglages modifiables depuis l'interface |
| `archive`, `fsutil`, `secretbox` | Archives sûres, écritures atomiques, chiffrement |
| `httpapi` | API REST, middlewares de sécurité, SSE |

Interface (`web/src/`) : `lib/` (client API, types, formatage, traductions `i18n.ts`), `locales/` (textes anglais et français par section, catalogue des messages du serveur généré), `hooks/`
(polling, statut global, console SSE, routage), `components/ui/` (primitives),
`components/hytale/` (connexion Hytale, étapes d'installation), `pages/`.
