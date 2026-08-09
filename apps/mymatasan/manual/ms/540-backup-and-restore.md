---
title: Menyandarkan konfigurasi anda
category: administration
categoryLabel: Pentadbiran
summary: Eksport kamera, peraturan, destinasi dan tetapan ke fail mudah alih — dan kembalikannya.
order: 540
---

# Menyandarkan konfigurasi anda

Sandaran konfigurasi ialah satu fail `.mmbackup` dilindungi frasa laluan yang memegang **tetapan**
anda, supaya mesin baharu boleh dihidupkan tanpa dikonfigurasikan semula.

Cipta dan pulihkannya dalam **Tetapan → Sandaran & Pemulihan**. Bestari larian pertama juga
menawarkan bahagian pemulihan — lihat [Memulihkan daripada sandaran](restore-from-backup).

## Memilih apa yang hendak disertakan {#sections}

Empat bahagian, boleh dipilih secara berasingan:

| Bahagian | Kandungan |
|---|---|
| **Kamera** | Entri kamera dan ONVIF termasuk kelayakan tersimpan, dan konfigurasi rakaman setiap kamera. |
| **Pengesanan AI** | Peraturan pengesanan dan daftar kelas objek. |
| **Pemberitahuan** | Destinasi penghantaran, termasuk rahsia Telegram, webhook dan MQTT. |
| **Tetapan aplikasi** | Tetapan penyahkod, penglihatan dan tangkapan; konfigurasi kesihatan kamera dan mesin. |

Ambil keempat-empatnya melainkan anda ada sebab untuk tidak. Sandaran separa adalah untuk
memindahkan satu bahagian konfigurasi antara mesin — mengangkat susunan pemberitahuan yang telah
ditala ke peranti kedua, contohnya.

## Apa yang tidak pernah disertakan {#excluded}

**Rakaman anda.** Sandaran ialah konfigurasi, bukan arkib. Rakaman, gambar petikan dan sejarah amaran
kekal di tempat ia dihasilkan.

**Identiti mesin ini.** Kunci penyulitan semasa rehat, gandingan dan pendaftaran armada, sijil mTLS,
`config.json` dan bendera persediaan-lengkap tidak pernah dieksport — jadi sandaran tidak boleh
digunakan untuk mengklon identiti satu peranti kepada peranti lain.

**Pemberat model tersuai.** Dirujuk, tidak dibenamkan. Salin fail `.pt` secara berasingan.

**Akaun pengguna tempatan.** Cipta semula pada mesin baharu.

Akibat praktikalnya: pembinaan semula penuh memerlukan *tiga* perkara — sandaran konfigurasi,
[kunci pemulihan](encryption-at-rest#export), dan rakaman itu sendiri. Kehilangan mana-mana satu
meninggalkan jurang yang tiada apa lagi mengisinya.

## Frasa laluan {#passphrase}

Fail itu membawa rahsia teks biasa yang tidak pernah dikeluarkan API biasa: kata laluan kamera, token
bot, kelayakan broker. Ia sentiasa disulitkan dengan frasa laluan yang anda tetapkan, dan **frasa
laluan itu tidak boleh dipulihkan**.

Oleh kerana penyulitannya berasaskan frasa laluan dan bukan terikat kepada mesin yang membuatnya, fail
itu boleh dibuka pada mana-mana hos. Itulah yang menjadikannya berguna untuk pemindahan dan berbahaya
jika ia terbocor. Anggap fail itu sebagai rahsia.

## Memulihkan {#restoring}

Muatkan fail, masukkan frasa laluannya, dan **pratonton** kandungannya sebelum menggunakan apa-apa.
Kemudian pilih:

- **Ganti** — menulis ganti bahagian yang ada dalam fail. Apa sahaja yang dikonfigurasikan dalam
  bahagian itu sekarang akan hilang.
- **Gabung** — menambah kandungan fail kepada yang sedia ada.

Gabung ialah pilihan selamat apabila memasukkan kamera satu tapak ke dalam peranti yang sudah
memerhati tapak lain. Ganti ialah apa yang anda mahukan apabila membina semula mesin kepada keadaan
yang diketahui.

Dalam kedua-dua mod, rujukan antara rekod ditunjuk semula semasa baris dimasukkan, jadi peraturan
tetap melekat pada kamera yang betul walaupun id dalamannya berubah.

## Selepas pemulihan {#after}

1. **Mulakan semula.** Perkhidmatan rakaman, pengesanan dan pemberitahuan mengambil konfigurasi lama
   semasa permulaan dan tidak akan menerima yang dipulihkan sehingga ia dimulakan semula. Laluan
   larian pertama meminta ini secara eksplisit.
2. **Semak kelayakan sampai.** Kata laluan kamera bergerak dalam fail itu; sahkan kamera dalam talian
   dan bukan sekadar menganggapnya.
3. **Lakukan semula apa yang sandaran tidak boleh bawa** — kunci penyulitan, gandingan armada, akaun
   pengguna, fail model tersuai.

## Bila hendak membuatnya {#when}

- Selepas persediaan larian pertama, sebaik tapak dikonfigurasikan.
- Selepas sebarang perubahan besar — kamera baharu, peraturan yang diubah, destinasi baharu.
- Sebelum naik taraf atau pemindahan perkakasan.

Sandaran adalah kecil dan tiada kos untuk menyimpannya. Simpan beberapa: sandaran yang dibuat selepas
salah konfigurasi memelihara salah konfigurasi itu dengan setia, dan satu-satunya jalan kembali ialah
yang lebih lama.

## Jika pemulihan gagal {#troubleshooting}

Hampir selalu salah satu daripada dua perkara, dan peranti tidak boleh memberitahu yang mana:

- **Frasa laluan salah** — fail itu langsung tidak boleh dinyahsulit.
- **Fail itu bukan sandaran**, atau terpotong semasa pemindahan.

Salin semula fail itu dan cuba frasa laluan sekali lagi. Sandaran yang frasa laluannya hilang tidak
boleh dibuka oleh sesiapa, dan itu memang direka begitu.
