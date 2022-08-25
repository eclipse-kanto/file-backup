![Kanto logo](https://github.com/eclipse-kanto/kanto/raw/main/logo/kanto.svg)

# Eclipse Kanto - File Backup

## Overview

The backup and restore functionality gives the ability to configure the edge from the backend to backup or restore folders, either on demand or periodically.

Backup and restore is based on file upload feature implementation.

Folders can be backed up or restored from different storage providers, currently including AWS, Azure and standard HTTP.

Backup and restore implements the [BackupAndRestore](https://github.com/eclipse/vorto/tree/development/models/com.bosch.iot.suite.manager.backup-BackupAndRestore-1.0.0.fbmodel) Vorto model.

### Capabilities include:

 * Backup - backup a local directory and upload it, using HTTP upload or Azure/AWS temporary credentials.
 * Restore - restore a local directory by downloading its content, using HTTP download or Azure/AWS temporary credentials.
 * Directory filter - select directory(or directories) to be backed up using glob pattern.
 * Keep uploaded backups - successfully uploaded backups can be either preserved on the file system or deleted after they are uploaded.
 * Storage - choose temporary local storage directory.
 * Command execution - execute a specific command before backup or restore operation.

## Community

* [GitHub Issues](https://github.com/eclipse-kanto/file-backup/issues)
* [Mailing List](https://accounts.eclipse.org/mailing-list/kanto-dev)