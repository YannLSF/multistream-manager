# Multistream Manager v0.4.2

WebUI autonome pour piloter plusieurs sorties RTMP/RTMPS à partir du flux Enhanced RTMP reçu par MediaMTX.

La v0.4.2 conserve l'architecture robuste validée jusque-là : un FFmpeg indépendant par destination, vidéo en copie directe dès que possible, aperçu HLS isolé et fonctions d'exploitation (authentification, métriques, logs rotatifs, historique, import/export et catalogue externalisé).

Cette version rend l'**adaptation audio automatique optionnelle par destination** et corrige le faux positif FFmpeg observé lors de l'arrêt normal du stream source.

## Nouveautés v0.4.2

### Adaptation audio automatique au choix

Chaque destination possède désormais une case **Adaptation audio automatique**, décochée par défaut.

- case décochée : l'audio sélectionné reste en `-c:a copy`, même si le preset indique par exemple une limite à 128 kb/s ; la WebUI affiche alors un avertissement explicite ;
- case cochée : si l'audio dépasse les contraintes connues et qu'une adaptation sûre est disponible, le Manager ne réencode que l'audio ;
- une piste déjà compatible reste toujours en copie directe, quelle que soit la case.

Le réglage est stocké dans `config.json` sous `auto_adapt_audio`. Une ancienne destination qui ne possède pas ce champ est chargée avec la valeur `false`, donc sans adaptation automatique.

Exemple avec une source AAC autour de 160 kb/s : Steam peut soit conserver ce flux tel quel, soit produire AAC 128 kb/s si la case est activée.

### Arrêt propre de la source OBS

Lorsque OBS cesse de publier, FFmpeg peut écrire :

```text
Error during demuxing: Input/output error
```

Pour notre entrée RTMP locale, cette ligne correspond généralement à la disparition normale de la source. La v0.4.2 la classe comme information et laisse au poller MediaMTX un court délai pour confirmer l'arrêt de la source. Si la source est effectivement hors ligne, aucun état `ERREUR`, retry ni événement d'historique n'est créé. Si la source reste en ligne, une sortie FFmpeg anormale continue d'être traitée comme une vraie panne.

### Authentification WebUI

L'authentification est activée dès que **les deux** variables suivantes sont définies :

- `AUTH_USERNAME`
- `AUTH_PASSWORD_HASH`

Le mot de passe en clair n'est jamais stocké par le Manager. `AUTH_PASSWORD_HASH` utilise le format PBKDF2-HMAC-SHA256 salé à 600 000 itérations.

La génération interactive sécurisée du hash, introduite en v0.4.1, reste disponible. Le mot de passe n'est ni passé dans les arguments du processus, ni placé dans une variable d'environnement, ni affiché par le terminal. Il est saisi deux fois avec l'écho désactivé.

Avec le binaire :

```bash
./multistream-manager --hash-password
```

Avec l'image Docker :

```bash
docker run --rm -it multistream-manager:v0.4.2 --hash-password
```

Le programme affiche :

```text
Mot de passe :
Confirmer le mot de passe :
$pbkdf2-sha256$600000$...$...
```

Les deux saisies sont invisibles. Seul le hash final est écrit sur la sortie standard.

Pour une automatisation non interactive, un mode distinct lit le secret uniquement depuis l'entrée standard :

```bash
docker run --rm -i multistream-manager:v0.4.2 --hash-password-stdin < /chemin/vers/un-secret-protege
```

Évite de construire cette entrée standard avec un mot de passe littéral dans la ligne de commande : cela annulerait l'intérêt du mode sécurisé en laissant potentiellement le secret dans l'historique du shell.

Le résultat ressemble à :

```text
$pbkdf2-sha256$600000$...$...
```

Il faut ensuite fournir **ce hash**, et non le mot de passe :

```bash
-e AUTH_USERNAME='admin' \
-e AUTH_PASSWORD_HASH='$pbkdf2-sha256$600000$...$...'
```

Les sessions WebUI utilisent un token aléatoire conservé uniquement en mémoire. Le cookie est `HttpOnly` et `SameSite=Strict`. Les sessions expirent après 24 h par défaut.

Variables associées :

- `AUTH_SESSION_HOURS` : durée de session, défaut `24`
- `AUTH_COOKIE_SECURE` : défaut `false`; mettre `true` lorsque la WebUI est servie en HTTPS

Si `AUTH_USERNAME` et `AUTH_PASSWORD_HASH` sont tous les deux absents, l'authentification reste désactivée pour conserver la compatibilité avec les installations locales existantes. Si une seule des deux variables est présente, le Manager refuse de démarrer.

Après cinq échecs de connexion depuis la même adresse, les nouvelles tentatives sont temporairement bloquées pendant 30 secondes.

> L'authentification ne remplace pas TLS. Si le port est accessible hors d'un LAN/VPN de confiance, place le Manager derrière un reverse proxy HTTPS et active `AUTH_COOKIE_SECURE=true`.

### Statistiques CPU / RAM

La WebUI affiche maintenant les ressources du Manager et de ses FFmpeg :

- processus Multistream Manager ;
- ensemble des forwards FFmpeg ;
- ensemble des aperçus HLS FFmpeg ;
- total ;
- CPU/RAM de chaque destination active.

Les métriques sont lues depuis `/proc` sous Linux. Le CPU d'un processus peut dépasser 100 % lorsqu'il utilise plusieurs cœurs, ce qui est normal pour cette représentation.

### Logs FFmpeg rotatifs

Les logs affichés dans la WebUI restent conservés dans une ring buffer bornée, mais les forwards écrivent aussi des fichiers persistants dans :

```text
/data/logs/<destination>.log
```

La rotation est effectuée par le Manager. Les clés de stream et le path MediaMTX actif sont masqués avant l'écriture sur disque.

Variables :

- `LOG_RING_LINES` : lignes conservées en mémoire par destination, défaut `500`
- `LOG_MAX_BYTES` : taille maximale d'un fichier avant rotation, défaut `2097152` (2 Mio)
- `LOG_BACKUPS` : nombre de sauvegardes rotatives, défaut `3`

Avec les valeurs par défaut, une destination peut donc avoir :

```text
kick.log
kick.log.1
kick.log.2
kick.log.3
```

Les logs du processus principal continuent d'être envoyés sur stdout/stderr afin que Docker ou le runtime du conteneur puisse appliquer sa propre politique de rotation.

### Historique persistant des erreurs

Les erreurs de forwards et d'aperçus sont enregistrées dans :

```text
/data/error-history.json
```

La WebUI possède un bouton **Historique** pour consulter les événements les plus récents et vider l'historique.

- horodatage ;
- destination ;
- type (`forward` ou `preview`) ;
- message ;
- numéro de tentative automatique lorsqu'il existe.

`ERROR_HISTORY_LIMIT` fixe le nombre maximal d'événements conservés, défaut `500`.

### Import / export de configuration

Le panneau **Gestion** permet :

- d'exporter le `config.json` courant ;
- d'importer un `config.json` précédemment exporté.

L'import remplace toutes les destinations et arrête proprement les forwards en cours avant de charger la nouvelle configuration.

**Attention : l'export de configuration contient les clés de stream.** Le fichier doit donc être traité comme un secret.

### Catalogue de presets séparé du binaire

Au premier démarrage de la v0.4.2, le catalogue embarqué est copié vers :

```text
/data/presets.json
```

Le Manager utilise ensuite ce fichier comme catalogue actif. Il n'est donc plus nécessaire de recompiler le binaire pour modifier une URL, une limite de bitrate ou ajouter un preset.

Depuis **Gestion**, il est possible de :

- exporter le catalogue courant ;
- importer un nouveau `presets.json` ;
- recharger `/data/presets.json` après une modification directe sur le volume.

Le changement prend effet immédiatement pour le diagnostic de compatibilité et pour les prochains démarrages de destination. Un forward déjà actif n'est pas redémarré automatiquement lorsqu'un preset change.

Un catalogue importé doit contenir des IDs uniques et conserver le preset `custom`.

## Comportement de streaming conservé depuis v0.3.2

Chaque destination possède toujours son **propre processus FFmpeg**. Il n'y a aucune mutualisation des transcodages audio à ce stade.

Pour chaque destination :

- vidéo compatible : `-c:v copy` ;
- audio compatible : `-c:a copy` ;
- audio incompatible : copie directe par défaut ; réencodage audio uniquement si **Adaptation audio automatique** est cochée ;
- vidéo incompatible : le Manager signale qu'un transcodage vidéo est requis, mais ne le lance pas automatiquement.

Exemple avec une source AAC autour de 160 kb/s / 48 kHz :

```text
Kick       adaptation décochée -> vidéo copy + audio copy
Steam      adaptation décochée -> vidéo copy + audio copy + avertissement
Steam      adaptation cochée   -> vidéo copy + AAC 128 kb/s
RUTUBE     adaptation cochée   -> vidéo copy + AAC 128 kb/s / 44,1 kHz
```

FFprobe pouvant observer un AAC nominal à 160 kb/s autour de 164 kb/s, le moteur applique une petite tolérance de mesure : 4 kb/s ou 2,5 %, le plus grand des deux.

## Aperçu HLS

Chaque destination conserve son aperçu local à la demande :

- FFmpeg distinct ;
- choix indépendant vidéo/audio ;
- vidéo et audio en copie directe ;
- playlist temporaire dans `/data/previews/<destination>` ;
- attente réelle du premier segment avant l'état **APERÇU ACTIF** ;
- arrêt automatique lorsque la fenêtre est fermée.

La WebUI charge actuellement `hls.js` depuis jsDelivr. Les navigateurs sans prise en charge HLS native ont donc besoin d'accéder à ce CDN.

## Mise à niveau depuis v0.3.2 / v0.4.x

Conserve simplement le même volume `/data`. Le format reste rétrocompatible ; la v0.4.2 ajoute seulement le champ booléen `auto_adapt_audio`. Pour une ancienne destination, ce champ absent vaut `false`. Pense donc à cocher **Adaptation audio automatique** sur les destinations pour lesquelles tu souhaites conserver le comportement de transcodage audio de la v0.4.1.

Construire :

```bash
cd ~/multistream-manager-v0.4.2
docker build -t multistream-manager:v0.4.2 .
```

Générer le hash du mot de passe avec saisie masquée et confirmation :

```bash
docker run --rm -it multistream-manager:v0.4.2 --hash-password
```

Puis lancer, par exemple :

```bash
docker stop multistream-manager 2>/dev/null || true

docker run --rm \
  --name multistream-manager \
  --network host \
  -v "$HOME/multistream-manager-data:/data" \
  -e MTX_API="http://127.0.0.1:9999" \
  -e MTX_RTMP_BASE="rtmp://127.0.0.1:1938" \
  -e MTX_PATH_PREFIX="app/" \
  -e AUTH_USERNAME="admin" \
  -e AUTH_PASSWORD_HASH='$pbkdf2-sha256$600000$REMPLACER$PAR_LE_HASH_GENERE' \
  multistream-manager:v0.4.2
```

Puis ouvrir :

```text
http://IP_DE_LA_VM_DOCKER:8090
```

Pour un reverse proxy HTTPS :

```bash
-e AUTH_COOKIE_SECURE=true
```

## Dossier `/data`

La v0.4.2 utilise :

```text
/data/config.json
/data/presets.json
/data/error-history.json
/data/logs/
/data/previews/
```

Les fichiers sensibles créés par le Manager utilisent le mode `0600`; les répertoires de données temporaires/logs sont créés avec des permissions restrictives.

## Variables d'environnement

### Source / exécution

- `BIND` : adresse HTTP, défaut `:8090`
- `DATA_DIR` : défaut `/data`
- `MTX_API` : défaut `http://127.0.0.1:9999`
- `MTX_RTMP_BASE` : défaut `rtmp://127.0.0.1:1938`
- `MTX_PATH_PREFIX` : défaut `app/`
- `POLL_SECONDS` : défaut `2`
- `FFMPEG_BIN` : défaut `ffmpeg`
- `FFPROBE_BIN` : défaut `ffprobe`

### Authentification

- `AUTH_USERNAME`
- `AUTH_PASSWORD_HASH`
- `AUTH_SESSION_HOURS` : défaut `24`
- `AUTH_COOKIE_SECURE` : défaut `false`

### Logs / historique

- `LOG_RING_LINES` : défaut `500`
- `LOG_MAX_BYTES` : défaut `2097152`
- `LOG_BACKUPS` : défaut `3`
- `ERROR_HISTORY_LIMIT` : défaut `500`

## Catalogue initial

Le catalogue initial v0.4.2 reprend les 51 presets de la v0.3.x, dont notamment Kick, Trovo, YouTube, Facebook, Instagram, TikTok, Telegram, LinkedIn, X, niconico, Nimo, FC2, GoodGame, Odysee, OK.ru, OnlyFans, Rumble, Vaughn, VK Video Live, Hou.la, Vimeo, Steam, RUTUBE, SOOP, Picarto, Twitch, DLive, Livepeer Studio, VRCDN, WPStream, Switchboard Live et Custom RTMP/RTMPS.

À partir de cette version, le fichier `/data/presets.json` est la source active et peut évoluer indépendamment du binaire.

## Limites connues

- Le transcodage vidéo reste un diagnostic uniquement.
- L'intervalle réel entre keyframes n'est pas encore mesuré automatiquement.
- Les transcodages audio identiques ne sont volontairement pas mutualisés avant l'audit de ressources en production.
- Les règles des plateformes peuvent évoluer ; le catalogue externalisé est justement prévu pour permettre leur correction sans rebuild.
- Les métriques CPU/RAM reposent sur `/proc` et ciblent donc le déploiement Linux/conteneur prévu par le projet.
- L'authentification protège l'application mais ne chiffre pas HTTP : utilisez HTTPS dès que le trafic quitte un réseau de confiance.
