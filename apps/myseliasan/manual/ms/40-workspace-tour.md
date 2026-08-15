---
title: Lawatan ruang kerja
category: getting-started
categoryLabel: Permulaan
summary: Untuk apa setiap bahagian skrin, dan mengapa menu anda berbeza daripada rakan sekerja.
order: 40
---

# Lawatan ruang kerja

Setiap skrin berkongsi bingkai yang sama: rel navigasi di satu sisi, jalur tindakan nipis di atas.

## Rel sisi {#side-rail}

**Ruang kerja**

- **Papan pemuka** — skrin ringkasan bagi seluruh armada.
- **Cerapan AI** — ringkasan bernarasi, dan sembang tanya-armada. Muncul hanya di mana diberikan.
- **Peta** — tapak anda pada peta geografi, dan pelan lantai di dalamnya.

**Armada**

- **Paparan langsung** — dinding video, merentasi setiap nod yang anda boleh lihat.
- **Objek** — cari apa yang dilihat kamera armada, merentasi nod.
- **Ajar** — ajar sebuah kamera sesuatu yang model asal tidak tahu.
- **Nod** — pepohon yang boleh dikembangkan. Akarnya menyenaraikan peranti anda; setiap nod di
  bawahnya membuka halaman nod itu sendiri, melalui terowong.
- **Peraturan armada** — satu-satunya entri yang mengenai armada *secara keseluruhan*: peraturan
  yang merentangi nod kamera dan hab penderia.

**Pentadbiran**

- **Pengguna**, **Peranan & Capaian**, **Log Audit**.

**Sistem**

- **Pemberitahuan** — suapan yang dikongsi, dengan kiraan belum dibaca.
- **Laporan** — PDF yang dijana.
- **Tetapan** — superadmin sahaja.
- **Bantuan** — manual ini.

## Mengapa menu anda berbeza daripada rakan sekerja {#menu-differences}

Inilah bahagian yang berbaloi difahami, kerana ia berfungsi berbeza di sini berbanding pada peranti
tunggal.

**Rel dibina daripada kebenaran anda.** Sesuatu entri muncul kerana peranan anda diberikan API di
sebaliknya — cabang Nod memerlukan capaian baca kepada nod, Cerapan AI memerlukan capaian kepada
ejen — bukan kerana bendera pada akaun anda. Menu dan penguatkuasaan datang daripada satu sumber,
jadi rel tidak boleh menawarkan sesuatu yang pelayan akan tolak.

Akibat praktikalnya: **jika anda menjangkakan sesuatu entri dan ia tiada, itu peranan anda.** Tanya
pentadbir dan bukan mencari halaman itu.

Dan tidak seperti perakam tunggal dengan tiga peranan tetap, peranan di sini adalah **milik anda
untuk ditakrifkan**. Satah kawalan biasanya mempunyai beberapa jenis pengguna — seseorang yang
memerhati, seseorang yang mentadbir sebuah tapak, seseorang yang hanya membaca laporan — jadi model
ini menjangkakan anda menciptanya dan bukan memilih daripada senarai.

## Capaian nod adalah berasingan {#node-access}

Sesuatu peranan memberikan capaian kepada *halaman*. Capaian kepada *nod tertentu* diberikan di
atasnya.

Pemisahan itulah yang membolehkan satu satah kawalan melayan beberapa tapak tanpa semua orang
melihat segalanya: sesuatu peranan boleh membawa paparan langsung dan pemberitahuan secara amnya,
sementara seseorang hanya diberikan dua nod di depoh tempat mereka benar-benar bekerja.

Nod yang baharu diambil oleh itu tidak automatik kelihatan kepada semua orang — lihat
[selepas pengambilan](adopting-nodes#after).

## Blok akaun {#account}

Di bawah logo: peranan anda dan butang log keluar. Ia memaparkan *peranan*, bukan identiti — satah
kawalan selalunya berada pada skrin yang orang lain boleh lihat, dan tiada sebab untuk
mengiklankan akaun mana yang sedang log masuk.

## Jalur atas {#header}

- **Bahasa** — Inggeris, Melayu, Cina, Arab. Bahasa Arab mencerminkan keseluruhan susun atur.
- **Tema** — cerah atau gelap.
- **?** — membuka manual ini pada halaman bagi apa sahaja yang sedang anda lihat.

Kedua-dua pilihan berkuat kuasa serta-merta dan diingati pada pelayar ini, jadi terminal yang
dikongsi boleh ditinggalkan dalam apa jua bahasa yang dibaca oleh orang yang menggunakannya.

## Perkara yang muncul di atas {#overlays}

- **Toast** — pengesahan ringkas di sudut; ia hilang dengan sendiri.
- **Kemajuan skrin penuh** — hanya untuk operasi yang tidak boleh diganggu, seperti mula semula
  selepas perubahan tetapan. Biarkan halaman terbuka; ia memuat semula dengan sendiri.
