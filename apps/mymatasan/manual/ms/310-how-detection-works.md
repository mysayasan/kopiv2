---
title: Bagaimana pengesanan berfungsi
category: detection
categoryLabel: Pengesanan & AI
summary: Model, label, keyakinan dan bingkai — idea yang anda perlukan sebelum menala apa-apa.
order: 310
---

# Bagaimana pengesanan berfungsi

Memahami artikel ini menjimatkan lebih banyak masa daripada mana-mana artikel lain dalam manual ini,
kerana hampir setiap masalah "AI ini salah" sebenarnya soal lapisan mana yang bersalah.

## Rantaian {#chain}

1. Satu **bingkai** ditarik daripada strim pengesanan kamera.
2. Setiap **model aktif** melihatnya dan melaporkan objek yang dikenalinya, setiap satu sebagai
   **label** (`person`, `car`, `truck`, …), satu **kotak**, dan satu **keyakinan** antara 0 dan 1.
3. Label mentah itu dipetakan kepada **kelas objek** yang anda namakan — *Orang*, *Kenderaan*,
   *Penghantaran*.
4. Setiap **peraturan** pada kamera itu memutuskan sama ada apa yang dilihat, di mana ia dilihat,
   bila, dan sejauh mana yakin, berbaloi menjadi satu **amaran**.

Model menghasilkan fakta. Peraturan menerapkan pertimbangan. Pisahkan kedua-duanya dan diagnosis
menjadi mudah:

- **Ia langsung tidak nampak benda itu** → model atau ambang keyakinan.
- **Ia nampak tetapi tidak memberi amaran** → peraturan: zon, jadual, pilihan kelas, bingkai
  minimum.
- **Ia memberi amaran pada perkara yang salah** → peraturan, hampir selalu: zon terlalu luas atau
  ambang terlalu rendah.

## Model {#models}

**Model asas (stok)** sentiasa hidup. Ia mengenali kelas harian yang umum — orang, kenderaan,
haiwan dan sebagainya. Saiznya ialah pertukaran langsung antara kelajuan dan ketepatan:

| Varian | Sifatnya |
|---|---|
| Nano | Terpantas, paling kurang tepat. Lalai, dan pilihan yang betul pada CPU atau Raspberry Pi. |
| Kecil | Sedikit lebih perlahan, ketara lebih baik. |
| Sederhana | Ketara lebih perlahan. GPU disyorkan. |
| Besar | Perlahan pada CPU. GPU disyorkan. |
| Sangat besar | Paling perlahan, paling tepat. GPU amat disyorkan. |

**Model tersuai** berjalan bersama model stok, bukan menggantikannya. Itulah yang membolehkan model
yang anda latih untuk satu objek khusus wujud bersama pengesanan umum.

Kosnya ialah bagi setiap model, bagi setiap bingkai: setiap model aktif membuat inferens pada setiap
bingkai. Dua model aktif kira-kira dua kali kerja satu. Menghidupkan model stok yang besar *dan* dua
model tersuai pada mesin sederhana ialah penjelasan biasa bagi sistem yang menjadi lembap.

Varian stok yang dikenali dimuat turun pada penggunaan pertama dan kemudian dicache, jadi muat turun
sekali itu memerlukan capaian internet. Selepas itu semuanya tempatan.

## Label dan kelas objek {#labels}

Model mengeluarkan **label mentah** — perkataan huruf kecil seperti `person`, `car`, `fire hydrant`.
Itu perbendaharaan kata model, dan ia tepat: label yang tidak dihasilkan mana-mana model aktif tidak
akan sekali-kali sepadan dengan apa-apa, dan label yang tersalah taip tidak sepadan dengan apa-apa
secara senyap.

Anda tidak menulis peraturan terhadap label mentah. Anda menulisnya terhadap **kelas objek** yang
anda takrifkan, yang memetakan nama mesra kepada satu atau lebih label mentah — *Kenderaan* = `car`,
`truck`, `bus`, `motorcycle`. Lihat [Kelas dan kumpulan objek](object-classes).

## Keyakinan {#confidence}

Keyakinan ialah sejauh mana pastinya model itu. **Ambang** sesuatu peraturan ialah minimum yang akan
diterimanya.

Tiada nilai yang betul, hanya pertukaran yang anda pilih:

- **Lebih rendah** menangkap lebih banyak peristiwa sebenar dan lebih banyak yang palsu.
- **Lebih tinggi** lebih senyap dan lebih banyak terlepas.

Mulakan sekitar lalai, perhatikan log amaran selama sehari, dan ubahnya. Ubahnya *kerana apa yang
anda lihat*, bukan atas prinsip. Kamera yang melihat pintu masuk yang cerah, dekat dan tidak
terhalang boleh menggunakan ambang tinggi; kamera yang melihat laluan masuk yang panjang dan gelap
tidak boleh, dan memaksakannya hanya bermakna ia tidak pernah dicetuskan.

## Bingkai minimum {#min-frames}

Berapa banyak bingkai berturut-turut mesti mengandungi objek itu sebelum peraturan dicetuskan.

Ini kawalan tunggal paling berkesan terhadap kelipan — bayang, rama-rama, artifak mampatan — kerana
bunyi jarang berterusan merentasi bingkai sedangkan manusia sebenar sentiasa berterusan. Menaikkannya
daripada satu kepada dua atau tiga menghapuskan kebanyakan amaran palsu dengan kos sepersekian saat.

## Tempoh sejuk {#cooldown}

Selepas peraturan dicetuskan, ia kekal senyap selama sekian saat.

Tanpanya, seorang yang berdiri di pintu menghasilkan satu amaran bagi setiap pengesanan selagi mereka
berdiri di situ. Tempoh sejuk menukar "satu peristiwa sedang berlaku" kepada "satu peristiwa telah
berlaku", dan itulah yang sepatutnya bermaksud pemberitahuan.

## Dari mana bingkai datang {#frames}

Pengesanan membaca strim **pengesanan** kamera, yang anda tetapkan pada
[tab Strim](camera-properties#stream) kamera. Ia tidak semestinya strim yang anda rakam.

Gunakan sub-strim. Pengesan tidak perlukan 4K untuk mengenali seseorang, dan perbezaan kos antara
sub-strim dan strim utama selalunya perbezaan antara mesin yang mampu dan yang tidak. Pengecualiannya
ialah [plat nombor](fire-smoke-and-plates#lpr), yang memerlukan setiap piksel yang boleh didapati.

## Apa yang ia tidak boleh lakukan {#limits}

- **Ia tidak memahami niat.** Ia mengenali seseorang; ia tidak boleh membezakan pemandu penghantaran
  daripada penceroboh. Zon, jadual dan kelas ialah cara anda membekalkan konteks itu.
- **Ia tidak boleh melihat apa yang kamera tidak boleh.** Tiada model memulihkan butiran daripada
  imej yang gelap, basah, bercahaya belakang atau tidak fokus. Penempatan kamera mengatasi saiz
  model, setiap kali.
- **Ia bukan manusia.** Setiap ambang ialah kebarangkalian. Bina proses anda atas andaian bahawa
  sesetengah amaran adalah salah dan sesetengah peristiwa terlepas, kerana kedua-duanya akan berlaku.
