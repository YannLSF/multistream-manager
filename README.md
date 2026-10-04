# Ylyxium Multistream Manager v0.5.0

Ylyxium Multistream Manager est une WebUI autonome permettant de piloter
plusieurs sorties RTMP/RTMPS à partir d'une ou plusieurs sources reçues par
MediaMTX.

Le projet est conçu en priorité pour les flux Enhanced RTMP / Enhanced
Broadcasting, tout en conservant la possibilité d'utiliser des flux RTMP
classiques compatibles.

La v0.5.0 introduit principalement :

- des distributions portables Windows x64 et Linux x64 ;
- FFmpeg, FFprobe et le MediaMTX Enhanced RTMP requis directement embarqués
  dans les archives portables ;
- la supervision automatique de MediaMTX en mode portable ;
- un mode bureau Windows avec icône dans la zone de notification ;
- la gestion de plusieurs sources MediaMTX ;
- le choix de la source indépendamment pour chaque destination ;
- le rebranding Ylyxium Multistream Manager ;
- une image Docker Manager mise à jour pour v0.5.0 ;
- la conservation du mode Docker avec MediaMTX externe.

## Architecture

Chaque destination possède son propre processus FFmpeg.

Le Manager ne mutualise pas les transcodages entre destinations.

Lorsqu'un flux est compatible avec la destination :

- la vidéo est copiée avec `-c:v copy` ;
- l'audio est copié avec `-c:a copy` ;
- si l'adaptation audio automatique est activée et nécessaire, seul l'audio
  est réencodé ;
- un transcodage vidéo nécessaire est actuellement signalé par le diagnostic,
  mais n'est pas lancé automatiquement.

Le MediaMTX utilisé par le projet est la version Enhanced RTMP personnalisée :

```text
MediaMTX v1.21.1-enhanced-rtmp.1
```

Elle utilise le gortmplib personnalisé correspondant :

```text
gortmplib v1.0.3-enhanced-rtmp.1
```

Un MediaMTX upstream standard ne remplace pas cette version pour les fonctions
Enhanced RTMP / multitrack utilisées par le projet.

## Sources MediaMTX

Deux familles de chemins sont utilisées par défaut.

### Source principale Twitch / Enhanced RTMP

```text
app/<cle-de-stream>
```

Cette famille correspond à la source principale historique.

La configuration MediaMTX portable générée par défaut contient un forward vers
l'ingest Twitch :

```text
app/<cle-de-stream>
    |
    +--> Twitch
```

Le forward conserve également la query string RTMP utilisée par Enhanced
Broadcasting.

Publier avec une vraie clé Twitch sous `app/*` peut donc démarrer une diffusion
réelle vers Twitch.

### Sources supplémentaires

```text
sources/<nom>
```

Exemples :

```text
sources/studio
sources/mobile
sources/vertical
```

Ces sources sont découvertes automatiquement par le Manager et ne sont pas
forwardées automatiquement vers Twitch.

Chaque destination Ylyxium peut sélectionner indépendamment la source à
utiliser.

## Mode portable

Les distributions portables sont disponibles pour :

- Windows x64 ;
- Linux x64.

Les archives contiennent le Manager ainsi que les binaires compatibles de
FFmpeg, FFprobe et du MediaMTX Enhanced RTMP utilisé par le projet.

Aucune installation globale de FFmpeg ou MediaMTX n'est nécessaire.

### Windows x64

Archive :

```text
Ylyxium-Multistream-Manager-v0.5.0-windows-x64.zip
```

Exécutable :

```text
YlyxiumMultistreamManager.exe
```

Après extraction, lancer :

```powershell
.\YlyxiumMultistreamManager.exe
```

Le mode Windows utilise une icône dans la zone de notification.

Le menu permet notamment :

- d'ouvrir le tableau de bord ;
- de consulter l'état du Manager ;
- de redémarrer le Manager ;
- d'afficher les informations de version ;
- d'arrêter proprement l'application.

Les processus enfants MediaMTX, FFmpeg et FFprobe sont lancés sans fenêtres
console supplémentaires.

Le tableau de bord est disponible par défaut sur :

```text
http://127.0.0.1:8090
```

### Linux x64

Archive :

```text
Ylyxium-Multistream-Manager-v0.5.0-linux-x64.tar.gz
```

Exécutable :

```text
YlyxiumMultistreamManager
```

Après extraction :

```bash
./YlyxiumMultistreamManager
```

Le tableau de bord est disponible par défaut sur :

```text
http://127.0.0.1:8090
```

### Arborescence portable

L'arborescence utilise notamment :

```text
YlyxiumMultistreamManager[.exe]
bin/
  ffmpeg[.exe]
  ffprobe[.exe]
  mediamtx[.exe]

data/
  config.json
  presets.json
  error-history.json
  mediamtx.yml
  previews/

logs/
  destinations/
```

Sous Windows, les journaux du Manager et du MediaMTX supervisé sont également
écrits dans le dossier `logs/`.

Les logs de destinations sont stockés sous :

```text
logs/destinations/
```

Le dossier `logs/` reste volontairement séparé de `data/` en mode portable.

## Supervision MediaMTX en mode portable

Lorsque les binaires portables requis sont présents, le Manager :

1. utilise les exécutables locaux de `bin/` ;
2. utilise `data/` comme dossier de données ;
3. utilise `logs/` comme dossier de logs ;
4. valide la configuration MediaMTX ;
5. démarre MediaMTX ;
6. attend que son API soit disponible ;
7. démarre ensuite la WebUI.

Ports par défaut :

```text
Manager HTTP : 8090
MediaMTX API : 9999
MediaMTX RTMP: 1938
```

Si une instance MediaMTX compatible répond déjà sur l'API configurée, le
Manager peut réutiliser cette instance externe au lieu d'en démarrer une
nouvelle. Une instance externe n'est alors pas arrêtée par le Manager.

## Mode Docker

En conteneur, Ylyxium Multistream Manager fonctionne en mode :

```text
system/container
```

FFmpeg et FFprobe proviennent de l'image Docker.

MediaMTX reste volontairement externe au conteneur du Manager.

Image Manager recommandée :

```text
ylyxium-multistream-manager:v0.5.0
```

Le Dockerfile conserve également l'ancien chemin interne :

```text
/usr/local/bin/multistream-manager
```

comme alias de compatibilité vers :

```text
/usr/local/bin/ylyxium-multistream-manager
```

### Construction de l'image Manager

Depuis le dépôt :

```bash
docker build \
  -t ylyxium-multistream-manager:v0.5.0 \
  .
```

### MediaMTX Docker

Le conteneur Manager n'embarque pas MediaMTX. Il faut donc construire ou
fournir séparément l'image Enhanced RTMP personnalisée.

La version validée pour v0.5.0 est :

```text
mediamtx:twitch-eb-1.21.1-enhanced-rtmp.1
```

Pour construire cette image depuis les sources figées :

```bash
git clone \
  --recurse-submodules \
  --branch v1.21.1-enhanced-rtmp.1 \
  https://github.com/YannLSF/mediamtx.git

cd mediamtx

docker build \
  -f Dockerfile.twitch \
  -t mediamtx:twitch-eb-1.21.1-enhanced-rtmp.1 \
  .
```

Les révisions validées sont :

```text
MediaMTX : 825b59f870bc1c493dd326e68dc33f81a0420263
gortmplib: cb3eae9f5733c14c01d19398520a140c8927c29b
```

Vérifier ensuite :

```bash
docker run --rm \
  mediamtx:twitch-eb-1.21.1-enhanced-rtmp.1 \
  --version
```

La sortie attendue est :

```text
v1.21.1-enhanced-rtmp.1
```

Le Manager doit pouvoir joindre :

```text
http://<conteneur-mediamtx>:9999
rtmp://<conteneur-mediamtx>:1938
```

MediaMTX doit donc autoriser l'accès API depuis le réseau Docker privé.

Exemple minimal adapté à cette architecture :

```yaml
logLevel: info

authMethod: internal
authInternalUsers:
  - user: any
    pass:
    ips: []
    permissions:
      - action: publish
      - action: read
      - action: playback
      - action: api

api: true
apiAddress: :9999

rtmp: true
rtmpEncryption: "no"
rtmpAddress: :1938

rtsp: false
hls: false
webrtc: false
srt: false
moq: false

paths:
  "~^app/(.+)$":
    forward:
      - dest: "rtmps://ingest.global-contribute.live-video.net/app#$G1?$MTX_QUERY"

  "~^sources/(.+)$": {}

  all_others:
```

Cette configuration est destinée à un réseau Docker privé dédié.

Il est recommandé de ne pas publier le port API MediaMTX `9999` sur l'hôte.

### Exemple Docker avec réseau privé

Créer un réseau :

```bash
docker network create ylyxium-multistream
```

Lancer le MediaMTX Enhanced RTMP :

```bash
docker run -d \
  --name ylyxium-mediamtx \
  --network ylyxium-multistream \
  -p 1938:1938 \
  -v "$PWD/mediamtx.yml:/mediamtx.yml:ro" \
  mediamtx:twitch-eb-1.21.1-enhanced-rtmp.1 \
  /mediamtx.yml
```

Le port `1938` est publié pour permettre à OBS ou à un autre encodeur d'envoyer
le flux RTMP.

Le port API `9999` reste uniquement accessible sur le réseau Docker privé.

Lancer le Manager :

```bash
docker run -d \
  --name ylyxium-multistream-manager \
  --network ylyxium-multistream \
  -p 8090:8090 \
  -v "$HOME/ylyxium-multistream-manager-data:/data" \
  -e MTX_API="http://ylyxium-mediamtx:9999" \
  -e MTX_RTMP_BASE="rtmp://ylyxium-mediamtx:1938" \
  -e MTX_PATH_PREFIX="app/" \
  -e MTX_SOURCES_PREFIX="sources/" \
  ylyxium-multistream-manager:v0.5.0
```

Ouvrir ensuite :

```text
http://IP_DU_SERVEUR:8090
```

## OBS

Avec les ports par défaut, le serveur RTMP est :

```text
rtmp://IP_DU_SERVEUR:1938/app
```

pour la source principale, ou :

```text
rtmp://IP_DU_SERVEUR:1938/sources
```

pour une source supplémentaire.

La clé de stream complète le chemin.

Exemples :

```text
rtmp://IP_DU_SERVEUR:1938/app/<cle>
rtmp://IP_DU_SERVEUR:1938/sources/studio
```

Attention : la publication sous `app/*` avec une vraie clé Twitch peut être
forwardée vers Twitch par la configuration MediaMTX par défaut.

## Aperçu HLS

Chaque destination peut utiliser un aperçu local à la demande avec :

- un processus FFmpeg distinct ;
- choix indépendant de la piste vidéo ;
- choix indépendant de la piste audio ;
- copie directe des flux compatibles ;
- playlist HLS temporaire ;
- attente du premier segment avant de déclarer l'aperçu actif ;
- arrêt lorsque l'aperçu n'est plus utilisé.

Les fichiers temporaires sont stockés sous `data/previews/` en mode portable
et sous `/data/previews/` en mode conteneur.

La WebUI charge actuellement `hls.js` depuis jsDelivr lorsque le navigateur ne
dispose pas d'une prise en charge HLS native.

## Adaptation audio

L'adaptation audio automatique est configurable indépendamment par
destination.

Si elle est désactivée :

```text
audio compatible   -> copie directe
audio incompatible -> copie directe + avertissement
```

Si elle est activée :

```text
audio compatible   -> copie directe
audio adaptable    -> réencodage audio automatique
```

La vidéo compatible reste en copie directe.

Le transcodage vidéo reste pour le moment un diagnostic uniquement.

## Authentification WebUI

L'authentification est activée lorsque les deux variables suivantes sont
définies :

```text
AUTH_USERNAME
AUTH_PASSWORD_HASH
```

Le Manager ne stocke pas le mot de passe en clair.

Le hash utilise PBKDF2-HMAC-SHA256 salé à 600 000 itérations.

### Générer un hash en mode portable

Linux :

```bash
./YlyxiumMultistreamManager --hash-password
```

Windows :

```powershell
.\YlyxiumMultistreamManager.exe --hash-password
```

### Générer un hash avec Docker

```bash
docker run --rm -it \
  ylyxium-multistream-manager:v0.5.0 \
  --hash-password
```

Mode non interactif :

```bash
docker run --rm -i \
  ylyxium-multistream-manager:v0.5.0 \
  --hash-password-stdin \
  < /chemin/vers/un-secret-protege
```

Ne place pas un mot de passe littéral dans la ligne de commande ou
l'historique du shell.

Fournir ensuite le hash :

```bash
-e AUTH_USERNAME="admin" \
-e AUTH_PASSWORD_HASH='$pbkdf2-sha256$600000$...$...'
```

Variables associées :

```text
AUTH_SESSION_HOURS
AUTH_COOKIE_SECURE
```

`AUTH_SESSION_HOURS` vaut `24` par défaut.

`AUTH_COOKIE_SECURE` vaut `false` par défaut et doit être activé lorsque la
WebUI est publiée derrière HTTPS.

Si une seule des deux variables `AUTH_USERNAME` et `AUTH_PASSWORD_HASH` est
présente, le Manager refuse de démarrer.

L'authentification ne remplace pas TLS. Lorsqu'une WebUI est accessible hors
d'un LAN ou VPN de confiance, utiliser un reverse proxy HTTPS et :

```text
AUTH_COOKIE_SECURE=true
```

## Données persistantes

### Portable

```text
data/config.json
data/presets.json
data/error-history.json
data/mediamtx.yml
data/previews/

logs/destinations/
```

### Docker / serveur

```text
/data/config.json
/data/presets.json
/data/error-history.json
/data/previews/
/data/logs/destinations/
```

`config.json` contient notamment les destinations et leurs clés de stream.

Les exports de configuration contiennent donc des secrets et doivent être
protégés comme tels.

`presets.json` est le catalogue actif des plateformes. Il peut être
exporté, importé ou modifié sans recompilation du Manager.

## Logs

Chaque destination possède son journal FFmpeg persistant.

En mode portable :

```text
logs/destinations/<destination>.log
```

En mode Docker / serveur :

```text
/data/logs/destinations/<destination>.log
```

La rotation est assurée par le Manager.

Variables :

```text
LOG_RING_LINES=500
LOG_MAX_BYTES=2097152
LOG_BACKUPS=3
ERROR_HISTORY_LIMIT=500
```

Les clés de stream et le chemin MediaMTX actif sont masqués avant l'écriture
des logs persistants.

L'historique des erreurs est conservé dans :

```text
data/error-history.json
```

ou :

```text
/data/error-history.json
```

selon le mode d'exécution.

## Import / export

Le panneau Gestion permet notamment :

- d'exporter `config.json` ;
- d'importer une configuration ;
- d'exporter le catalogue `presets.json` ;
- d'importer un catalogue ;
- de recharger le catalogue actif ;
- de consulter et vider l'historique des erreurs.

L'import d'une configuration remplace les destinations existantes et arrête
proprement les forwards concernés avant le chargement.

## Catalogue de plateformes

Le catalogue embarqué couvre de nombreuses plateformes RTMP/RTMPS, notamment :

- Kick ;
- Trovo ;
- YouTube Live ;
- Facebook Live ;
- Instagram Live ;
- TikTok LIVE ;
- Telegram Live ;
- LinkedIn Live ;
- X / Media Studio ;
- niconico Live ;
- Nimo TV ;
- FC2 Live ;
- GoodGame ;
- Odysee ;
- OK.ru ;
- OnlyFans ;
- Rumble ;
- Scoop Live ;
- Vaughn Live ;
- VK Video Live ;
- Hou.la ;
- Vimeo Live ;
- Steam Broadcasting ;
- RUTUBE ;
- SOOP / AfreecaTV ;
- Picarto ;
- Piczel.tv ;
- Mixcloud Live ;
- Aparat ;
- KakaoTV ;
- Boosty ;
- Twitch ;
- DLive ;
- Livepeer Studio ;
- VRCDN ;
- différents services de relay et événementiels ;
- RTMP / RTMPS personnalisé.

Les contraintes des plateformes peuvent évoluer.

Le fichier actif `presets.json` permet de corriger ou d'étendre le catalogue
sans reconstruire le binaire.

## Variables d'environnement

### Serveur

```text
BIND=:8090
DATA_DIR=/data
```

### MediaMTX

```text
MTX_API=http://127.0.0.1:9999
MTX_RTMP_BASE=rtmp://127.0.0.1:1938
MTX_PATH_PREFIX=app/
MTX_SOURCES_PREFIX=sources/
POLL_SECONDS=2
```

### Binaires

```text
FFMPEG_BIN=ffmpeg
FFPROBE_BIN=ffprobe
```

Les distributions portables utilisent automatiquement les binaires contenus
dans leur dossier `bin/`.

### Authentification

```text
AUTH_USERNAME
AUTH_PASSWORD_HASH
AUTH_SESSION_HOURS=24
AUTH_COOKIE_SECURE=false
```

### Logs et historique

```text
LOG_RING_LINES=500
LOG_MAX_BYTES=2097152
LOG_BACKUPS=3
ERROR_HISTORY_LIMIT=500
```

## Mise à niveau depuis v0.4.x

Les données v0.4.x restent réutilisables.

### Depuis Docker

Conserver le volume `/data`.

Puis remplacer l'ancienne image Manager par :

```text
ylyxium-multistream-manager:v0.5.0
```

Le binaire interne historique :

```text
/usr/local/bin/multistream-manager
```

reste disponible sous forme d'alias pour limiter les ruptures de scripts.

La v0.5.0 ajoute la notion de source par destination. Les anciennes
destinations sans source explicite utilisent la source principale.

Les destinations sans champ `auto_adapt_audio` continuent à charger ce réglage
à `false`.

Pour une topologie Docker en conteneurs séparés, ne pas utiliser
`127.0.0.1` comme adresse MediaMTX dans le Manager : utiliser le nom du
conteneur sur un réseau Docker commun.

### Vers le mode portable

Extraire l'archive v0.5.0 dans un nouveau dossier.

Les données persistantes du Manager peuvent ensuite être importées depuis
l'ancienne installation.

Ne remplace pas arbitrairement les exécutables FFmpeg, FFprobe ou MediaMTX de
l'archive portable : les versions incluses ont été validées ensemble.

## Composants embarqués

Les distributions portables v0.5.0 embarquent notamment :

```text
Ylyxium Multistream Manager 0.5.0
MediaMTX 1.21.1-enhanced-rtmp.1
FFmpeg / FFprobe 9.0
```

Les fichiers de licences et notices des composants tiers distribués sont
inclus dans les archives portables.

Consulter notamment le dossier `licenses/` et les notices tierces fournies
avec la distribution.

## Sécurité

Quelques règles importantes :

- traiter `config.json` et ses exports comme des secrets ;
- ne pas exposer l'API MediaMTX `9999` publiquement ;
- utiliser un réseau Docker privé entre Manager et MediaMTX ;
- protéger la WebUI par authentification lorsqu'elle n'est pas strictement
  locale ;
- utiliser HTTPS dès que le trafic quitte un réseau local ou VPN de confiance ;
- ne pas publier une vraie clé Twitch sous `app/*` pendant un test si une
  diffusion réelle n'est pas souhaitée.

## Limites connues

- le transcodage vidéo automatique n'est pas encore implémenté ;
- l'intervalle réel entre keyframes n'est pas encore mesuré automatiquement ;
- les transcodages audio identiques ne sont pas mutualisés ;
- les règles et limites des plateformes peuvent évoluer ;
- les métriques processus détaillées restent principalement adaptées au mode
  Linux / conteneur ;
- l'aperçu HLS peut nécessiter l'accès à jsDelivr pour charger `hls.js`.

## Développement et release

Les builds portables officiels sont produits depuis un arbre Git propre et
contiennent un manifeste de build ainsi que les licences tierces nécessaires.

Une release v0.5.0 doit être construite depuis le commit exact correspondant au
tag `v0.5.0`.

Les SHA256 publiés avec les archives doivent être vérifiés avant distribution.

Le tag et la release ne doivent être créés qu'après validation des builds
Windows, Linux et Docker.
