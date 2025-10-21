# Cloud Hypervisor POC

Check that we can run Chrome in a Cloud Hypervisor VM, using our existing image builds. Investigate performance and behavior of Cloud Hypervisor.

## Initial setup:

Download a kernel, we will use this when we start the VM:

```bash
./get-kernel.sh 
```

This will result in the kernel at `./vmlinux`

## Build initial ram disk, and build Chromium root filesystem

To minimize the amount of memory required, which speeds up the snapshot / restore process, the boot process works with 3 main flags.

```
# shown to explain, not to run
...
  --kernel vmlinux \
  --initramfs initrd \
  --disk path=rootfs.ext4,readonly=on \
...
```

We provide the kernel because docker filesystem don't include kernels, so this is analogous to docker to provide a kernel separately from root file system.

In unikraft world, we pack the chromium root filesystem into the initial ram disk. In this setup, we instead build a busybox root filesystem with an an initialzation script in our inital ram disk. The init script here sets up an in-memory overlay filesystem on top of the attached, read-only chromium root fs disk. Then, we switch root into the overlay file system. This avoids keeping the entire Chromium filesystem in memory, and Chrome is starting up in these VMs with only 2GB of memory (might work with less, didn't try that).

To build the initial ram disk and chromium root filesystem, run this (check notes below first):

```bash
./build-initrd.sh
```
Note: this assumes you have kernel-images-private cloned to your home directory.
Note: Also you have to install `iproute2` in the Chromium headful image, see this diff
```
-    wget ca-certificates python2 supervisor xclip xdotool \
+    wget ca-certificates python2 supervisor xclip xdotool iproute2 \
```

When that is done, you will have `initrd` and `rootfs.ext4`.

In this current setup, configuration (envs) are built directly into the init script, but in a real setup we would probably mount another disk for this so that the initrd and rootfs stay the same for all VMs on the same container version.

## Run Chromium

First, configure the host's network to provide a tap interface for the guest VM.

```bash
# this is idempotent
./setup-host-network.sh
```
Note: more investigation is needed before using this networking setup for production.

Next, start Chromium! Note that you will want another shell after this step, so start a tmux session or something before running this.

```bash
./start-chrome.sh
```

## Perform a snapshot

While chrome is running in the VM in another shell, take a snapshot:

```bash
./snapshot.sh
```

Now that you have it snapshot, you can kill the VM like this:

```bash
sudo pkill cloud-hyper
```

## Perform a restore

Back in your original shell where you were running the Chrome VM, start a hypervisor without booting a VM:

```bash
./prepare-restore.sh
```
This script just starts up cloud hypervisor without any settings, so it's ready to receive the restore.

Then, perform the restore from your second shell:
```bash
./remote-restore.sh
```

Note, the way this works is sending a HTTP request to the socket. We don't have to use the `ch-remote` command line tool, we can call the API directly, like how it's done in **virtink** project, see the sample code in `virtink-reference.sh`. This file shows an example of how the virtink project takes a docker image and boots it into a cloud hypervisor VM.

## Next steps

- Can we connect to webRTC?
- Can we install sshd, ssh into the instance?
- Can we connect to CDP and do something?
- Does standby / restore work without interrupting CDP connection?
