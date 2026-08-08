---
title: Memulihkan daripada sandaran
category: getting-started
categoryLabel: Permulaan
summary: Hidupkan mesin baharu dengan kamera, peraturan dan tetapan pemasangan sedia ada tanpa perlu mengkonfigurasinya semula.
order: 40
---

# Memulihkan daripada sandaran

Sandaran konfigurasi ialah satu fail `.mmbackup` yang memegang **tetapan** sesuatu pemasangan —
kameranya, peraturan pengesanannya, destinasi pemberitahuannya, konfigurasi runtimenya. Memulihkan
satu daripadanya membolehkan mesin baharu mengambil alih semua itu, bukannya dikonfigurasikan secara
manual buat kali kedua.

## Apa yang ada dan tiada dalam fail itu {#contents}

Sandaran boleh membawa mana-mana daripada empat bahagian:

| Bahagian | Apa yang dipegangnya |
|---|---|
| **Kamera** | Entri kamera dan ONVIF termasuk kelayakan yang disimpan, serta konfigurasi rakaman setiap kamera. |
| **Pengesanan AI** | Peraturan pengesanan dan daftar kelas yang dirujuknya. |
| **Pemberitahuan** | Destinasi penghantaran, termasuk rahsia Telegram, webhook dan MQTT. |
| **Tetapan aplikasi** | Tetapan penyahkod, penglihatan dan tangkapan; konfigurasi kesihatan kamera dan mesin. |

Dua perkara sengaja tiada.

**Rakaman anda.** Sandaran ialah konfigurasi, bukan arkib. Rakaman, gambar petikan dan sejarah
amaran kekal pada mesin yang menghasilkannya.

**Identiti mesin ini.** Kunci penyulitan semasa rehat, gandingan dan pendaftaran armada, sijil mTLS,
`config.json` dan bendera persediaan-lengkap tidak pernah dieksport. Itu disengajakan: ia bermakna
sandaran tidak boleh digunakan untuk mengklon identiti satu peranti kepada peranti yang lain.

> [!WARNING]
> Fail ini mengandungi kelayakan teks biasa yang tidak pernah diberikan oleh API biasa — kata laluan
> kamera, token bot, rahsia broker. Ia disulitkan dengan frasa laluan pilihan anda, dan frasa laluan
> itulah satu-satunya perlindungannya. Anggap fail itu sebagai rahsia dan pilih frasa laluan
> sewajarnya.

Oleh kerana penyulitannya berasaskan frasa laluan dan bukan terikat kepada mesin yang membuatnya,
fail itu boleh dibuka pada mana-mana hos. Itulah yang menjadikannya berguna untuk pemindahan dan
itulah yang menjadikannya berbahaya jika ia terbocor.

## Memulihkan semasa persediaan kali pertama {#during-setup}

Pada langkah Selamat Datang dalam bestari, pilih **Pulih daripada sandaran**, pilih failnya, dan
masukkan frasa laluannya.

Laluan ini menggunakan sandaran secara terus, menggantikan apa sahaja yang ada pada pemasangan
baharu itu. Tiada pratonton — pada mesin yang baharu, tiada apa untuk ditimbang bandingkannya.
Kemudian:

1. Pemulihan melaporkan kejayaan.
2. **Mulakan semula untuk menggunakannya.** Ini bukan pilihan. Perkhidmatan rakaman, pengesanan dan
   pemberitahuan dimulakan terhadap konfigurasi lama yang kosong dan tidak akan mengambil konfigurasi
   yang dipulihkan sehingga ia dimulakan semula. Halaman akan dimuat semula dengan sendirinya sebaik
   peranti kembali.
3. Bestari tidak muncul semula. Mesin yang dipulihkan dianggap sudah dikonfigurasikan.

Selepas itu, semak perkara yang tidak boleh dibawa oleh sandaran: penyulitan semasa rehat dan kunci
pemulihannya, serta gandingan armada jika nod ini milik satah kawalan.

## Memulihkan ke dalam pemasangan yang sedang berjalan {#into-running}

Pada peranti yang sudah disediakan, pulihkan daripada **Tetapan → Sandaran & Pemulihan**
sebaliknya. Laluan itu memaparkan pratonton kandungan fail sebelum menggunakan apa-apa, dan memberi
anda dua mod:

- **Ganti** menulis ganti bahagian yang ada dalam fail. Apa sahaja yang dikonfigurasikan dalam
  bahagian itu sekarang akan hilang.
- **Gabung** menambah kandungan fail kepada apa yang sedia ada.

Gabung ialah pilihan lalai yang selamat apabila anda menarik kamera satu tapak ke dalam peranti yang
sudah memerhati tapak lain. Ganti ialah apa yang anda mahukan apabila anda membina semula mesin
kepada keadaan yang diketahui.

Dalam kedua-dua mod, rujukan antara rekod ditunjuk semula semasa baris dimasukkan, jadi peraturan
tetap melekat pada kamera yang betul walaupun id dalaman kamera itu berubah.

## Jika pemulihan gagal {#troubleshooting}

Hampir setiap kegagalan ialah salah satu daripada dua perkara, dan peranti tidak boleh memberitahu
anda yang mana satu:

- **Frasa laluan salah.** Fail itu langsung tidak boleh dinyahsulit.
- **Fail itu bukan sandaran**, atau terpotong semasa pemindahan.

Muat turun atau salin semula fail itu dan cuba frasa laluan sekali lagi. Sandaran yang frasa
laluannya hilang tidak boleh dibuka — tiada laluan pemulihan, dan itu memang direka begitu.
