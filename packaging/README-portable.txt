Ylyxium Multistream Manager v0.5.0
==========================

Ylyxium Multistream Manager is a standalone WebUI for managing multiple RTMP/RTMPS
outputs from Enhanced RTMP sources received by MediaMTX.

This portable distribution contains:

  - Ylyxium Multistream Manager v0.5.0
  - MediaMTX v1.21.1-enhanced-rtmp.1
  - FFmpeg / FFprobe n9.0.2-17-g2a571b6068-20260930

No Docker installation is required.

DIRECTORY LAYOUT
----------------

MultistreamManager
or
MultistreamManager.exe

bin/
  ffmpeg / ffmpeg.exe
  ffprobe / ffprobe.exe
  mediamtx / mediamtx.exe

data/
  Runtime configuration and persistent data.

logs/
  Runtime logs.
  Destination FFmpeg logs are stored in logs/destinations/.

licenses/
  Third-party license notices.

BUILD-MANIFEST.txt
  Exact versions, source revisions and SHA256 hashes.


STARTING THE APPLICATION
------------------------

Linux:

  ./MultistreamManager

Windows:

  MultistreamManager.exe

The WebUI listens on port 8090 by default.

Open:

  http://127.0.0.1:8090/


RTMP INPUT
----------

The bundled MediaMTX instance listens for RTMP sources on port 1938.

Default primary source:

  rtmp://HOST:1938/app/STREAM_KEY

Additional sources can use:

  rtmp://HOST:1938/sources/STREAM_KEY


PORTABLE DATA
-------------

When ffmpeg, ffprobe and mediamtx are found in the local bin directory,
Ylyxium Multistream Manager automatically enables portable mode.

Persistent data is then stored in:

  data/

This includes configuration, presets, preview files and error history.


PORTABLE LOGS
-------------

Portable builds keep logs outside the data directory:

  logs/

Destination-specific FFmpeg logs are stored in:

  logs/destinations/

The Windows desktop build also uses this directory for the main application
and managed MediaMTX logs.

The log directory can be overridden with:

  LOG_DIR


EXTERNAL MEDIAMTX
-----------------

The bundled MediaMTX instance is started automatically in portable mode.

If the configured MediaMTX API is already available when Ylyxium Multistream Manager
starts, the existing external instance is used instead.

Ylyxium Multistream Manager does not take ownership of an already-running external
MediaMTX process and will not stop it when Ylyxium Multistream Manager exits.


LINUX REQUIREMENTS
------------------

Architecture:

  x86-64

FFmpeg build target:

  Linux kernel >= 4.18
  glibc >= 2.28

The Linux package is intended for normal glibc-based distributions such as
recent Debian, Ubuntu, Fedora, RHEL-compatible distributions and similar
systems.

Alpine Linux / musl is not a supported native target for this package.


WINDOWS REQUIREMENTS
--------------------

Architecture:

  x86-64

Windows 10 22H2 or newer is the supported target for the bundled FFmpeg build.


SECURITY
--------

Web authentication is disabled by default.

For installations exposed outside a trusted LAN/VPN, configure:

  AUTH_USERNAME
  AUTH_PASSWORD_HASH

and use HTTPS through an appropriate reverse proxy.

Configuration exports can contain stream keys and must be treated as secrets.


THIRD-PARTY SOFTWARE
--------------------

FFmpeg and FFprobe are separate executables distributed from the
BtbN/FFmpeg-Builds project.

MediaMTX is a separate executable based on MediaMTX v1.21.1 with custom
Enhanced RTMP multitrack support.

See:

  licenses/FFMPEG-LICENSE.txt
  licenses/MEDIAMTX-LICENSE.txt
  THIRD-PARTY-NOTICES.txt
  BUILD-MANIFEST.txt

WINDOWS GO DEPENDENCY LICENSES
------------------------------

The Windows portable archive additionally contains the license texts for the
Go dependencies embedded by the desktop / notification-area build:

  licenses/FYNE-SYSTRAY-LICENSE.txt
  licenses/GOLANG-X-SYS-LICENSE.txt
  licenses/GODBUS-DBUS-LICENSE.txt

These Go dependencies are not linked into the Linux manager binary.
