---
title: Peta armada
category: map
categoryLabel: Peta & tapak
summary: Di mana sebenarnya peranti anda berada — dan cara peta berfungsi tanpa internet.
order: 210
---

# Peta armada

**Map** memaparkan armada anda sebagai tempat dan bukan baris: setiap nod di tapak tempat ia berdiri,
diwarnakan mengikut keadaannya.

Senarai memberitahu anda sebuah nod hilang. Peta memberitahu anda *bangunan mana yang kini tidak
diawasi*, dan itulah soalan yang orang benar-benar tanya semasa insiden.

## Meletakkan nod {#placing}

Nod yang baru diambil bermula dalam **Unplaced nodes**. Seret ia ke atas peta untuk meletakkannya.

Sebelum anda berbuat demikian, ia hanyalah baris dalam senarai dan bukan sebuah tempat — sebab
itulah pengambilan belum benar-benar selesai sehingga nod itu berada di atas peta.

Apabila nod berdekatan antara satu sama lain, ia berkumpul menjadi kelompok. **Zum masuk** untuk
mengalihkan satu nod keluar daripada kelompok; pada zum rendah anda sebenarnya menyeret kumpulan
itu, bukan perantinya.

## Membaca penanda {#legend}

| Warna | Maksud |
|---|---|
| **Online** | Bersambung dan melapor. |
| **Cert expiring** | Masih berfungsi, tetapi sijilnya hampir habis. |
| **Lost** | Tidak bersambung. |
| **Idle** | Telah diambil, tiada apa untuk dilapor. |

"Cert expiring" mendapat warnanya sendiri kerana itulah kegagalan yang masih boleh anda cegah —
lihat [Mengurus nod](managing-nodes#certificate).

## Peta asas, dan bekerja tanpa internet {#basemap}

Peta mempunyai dua bahagian yang bebas antara satu sama lain: **data anda** (nod, bangunan,
penempatan) dan **peta asas** (jalan dan rupa bumi di bawahnya).

Data anda sentiasa berfungsi. Peta asas ialah fail **PMTiles** yang disajikan sendiri oleh satah
kawalan, dan tanpanya peta akan berkata *no offline basemap installed — showing node positions
only*. Itu peta yang terhad, bukan peta yang rosak: penanda, bangunan dan pelan lantai semuanya
tetap berfungsi, cuma tiada jalan di belakangnya.

Pembahagian inilah keseluruhan reka bentuknya. Satah kawalan terasing tidak boleh mengambil jubin
peta daripada internet atas permintaan seperti peta web, jadi jubin itu mesti berada **pada peranti
itu sendiri**.

## Memuat turun sesuatu wilayah {#region-download}

Jika satah kawalan memang mempunyai akses internet, anda boleh mengekstrak sesuatu wilayah ke dalam
peta asas tempatan: tetapkan URL sumber PMTiles jauh sekali sahaja, kemudian muat turun kawasan yang
sedang anda lihat.

Dua perkara perlu diketahui sebelum mencuba:

- **Memuat turun akan menghubungi URL itu melalui internet.** Di tapak yang sepatutnya tiada trafik
  keluar, inilah perkara yang tidak patut dilakukan — pasang fail peta asas yang telah disediakan
  sebagai ganti.
- **Alat `pmtiles` mesti dipasang pada pelayan.** Jika ia tiada, halaman ini menyatakannya dan muat
  turun akan gagal sehingga ia dipasang.

Jika muat turun ditolak kerana kawasan terlalu besar, zum masuk dan ambil sedikit demi sedikit.

## Daripada peta ke dalam bangunan {#indoor}

Bangunan yang telah diletakkan akan membuka **pelan lantainya**, dan dari situ anda melihat kamera
yang disemat pada bilik dan bukan pin di atas jalan.

Itulah gerak menyelami yang berbaloi dipelajari: tapak → bangunan → lantai → kamera, iaitu cara
seseorang menyebut lokasi dengan mulut, dan kini juga cara anda melayarinya. Lihat
[Bangunan dan pelan lantai](buildings-and-floors).

## Tapak {#sites}

Tapak mengumpulkan segala yang berada di satu lokasi. Tapis peta mengikut tapak untuk bekerja pada
satu tempat pada satu masa, dan gunakan tapak untuk mengekalkan armada berbilang lokasi supaya
mudah dibaca — laporan juga boleh dihasilkan mengikut tapak atas sebab yang sama.
