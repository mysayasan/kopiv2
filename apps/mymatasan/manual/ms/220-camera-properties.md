---
title: Halaman kamera, tab demi tab
category: cameras
categoryLabel: Kamera
summary: Rujukan untuk Butiran, Akses, Strim, Rakaman dan ONVIF — serta memilih strim yang betul bagi setiap tugas.
order: 220
---

# Halaman kamera, tab demi tab

Memilih kamera pada rel membuka halamannya sendiri: pratonton langsung, peraturan Pengesanan AI dan
log amarannya, serta ruang Tetapan yang dipecahkan kepada lima tab.

## Butiran {#details}

Nama, penerangan dan identiti asas kamera.

Penerangan itu berbaloi diisi. Ia muncul sebagai tip alat pada rel, dan *"meliputi pintu pagar dan
sepuluh meter pertama laluan masuk"* menjimatkan perjalanan orang seterusnya.

**Zon Bahaya** di bahagian bawah membuang kamera — lihat
[Membuang kamera](adding-cameras#removing).

## Akses {#access}

Segala yang berkaitan kelayakan.

**Nama pengguna dan kata laluan kamera** ialah yang digunakan peranti untuk mencapai kamera.
Tukarkannya di sini apabila ia berubah pada kamera. Kata laluan yang disimpan tidak pernah
ditunjukkan semula kepada anda; medan itu hanya menyatakan ada satu yang tersimpan.

**Tukar kata laluan kamera** pula bergerak ke arah bertentangan: ia menukar kata laluan *pada kamera
itu sendiri* melalui ONVIF, dan mengemas kini salinan tersimpan supaya sepadan.

**Pengguna Kamera** menguruskan akaun ONVIF kamera itu sendiri — senaraikan, tambah, buang, tukar
kata laluan. Peranan ialah peranan kamera itu (Administrator, Operator, User), bukan peranan peranti
ini.

**Audio dua hala** ialah tempat cakap-balas dikonfigurasikan, termasuk kata laluan pembesar suara.
Kata laluan yang diperlukan bergantung pada jenama, dan ia tidak selalunya kata laluan penstriman —
lihat [Cakap-balas](live-views#talk-back).

## Strim {#stream}

Tab yang menentukan kualiti gambar dan sekeras mana mesin bekerja.

**Cari Strim** bertanya kamera apa yang ditawarkannya. Kamera biasa mempunyai dua atau tiga profil:
strim utama beresolusi tinggi dan satu atau dua sub-strim yang lebih kecil. Setiap profil menunjukkan
URI RTSP dan trek-treknya, dan **Uji RTSP** membuktikan salah satunya benar-benar boleh dimainkan
sebelum anda menetapkannya.

Anda kemudian menetapkan profil kepada empat peranan:

| Peranan | Apa yang disuapnya | Apa yang patut dipilih |
|---|---|---|
| **Paparan langsung** | Dinding video | Sub-strim. Anda memerhati jubin, bukan skrin pawagam. |
| **Pengesanan** | Pengesan AI | Sub-strim, biasanya. Pengesan tidak perlukan 4K untuk melihat seseorang, dan bingkai yang lebih kecil jauh lebih murah. |
| **Rakaman** | Rakaman pada cakera | Strim utama. Inilah yang akan anda juling melihatnya kemudian. |
| **Sandaran** | Digunakan apabila strim pilihan gagal | Apa sahaja yang berfungsi dengan boleh dipercayai. |

Menyalahkan hal ini ialah punca tunggal paling biasa bagi "mesin tidak dapat menampung". Menghalakan
pengesanan ke strim utama 4K boleh memakan CPU beberapa kali ganda berbanding sub-strim tanpa
sebarang keuntungan pada apa yang dikesan.

Pengecualiannya ialah [plat nombor](fire-smoke-and-plates#lpr), yang memang memerlukan resolusi dan
akan menggunakan strim tertinggi kamera secara automatik.

## Rakaman {#recording}

Rakaman bagi setiap kamera: sama ada ia merakam langsung, panjang segmen, pra-gulung dan
pasca-gulung untuk klip peristiwa, pengekalan dalam hari, dan laluan storan. Di sinilah juga
**Rakam metadata objek** berada — lihat
[Mencari apa yang dilihat kamera anda](object-search).

Diliputi sepenuhnya dalam [Konfigurasi rakaman](recording-configuration).

## ONVIF {#onvif}

Pengurusan peranti: apa yang dilaporkan kamera tentang dirinya dan apa yang boleh anda ubah padanya —
jam, rangkaian, pengguna, but semula dan set semula kilang.

Diliputi dalam [Menguruskan kamera melalui ONVIF](onvif-management).

## Pengesanan AI {#ai-detection}

Bukan tab tetapan tetapi halamannya sendiri pada kamera: peraturan pengesanan bagi kamera ini dan
log amaran penuh. Lihat [Mencipta peraturan pengesanan](detection-rules).

## Pratonton langsung dan zon pengesanan {#preview}

Pratonton pada halaman kamera turut berfungsi sebagai kanvas untuk melukis zon pengesanan dan garis
lintasan.

Satu perkara mengelirukan semua orang sekali: **pratonton ialah gambar petikan berkala, bukan video
langsung.** Ia kelihatan tersekat-sekat dan berhenti sementara anda menyeret. Pengesanan itu sendiri
berjalan pada kadar penuh pada strim sebenar — zon yang anda lukis tidak memberi kesan kepada
kelajuan atau ketepatan pengesanan, dan pratonton yang tersekat-sekat itu tidak memberitahu apa-apa
tentang prestasi pengesan.
