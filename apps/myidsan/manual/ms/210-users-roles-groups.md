---
title: Pengguna, peranan dan kumpulan
category: people
categoryLabel: Orang dan akses
summary: Melepaskan akaun yang tertunggak, dan bagaimana peranan bertukar menjadi menu.
order: 210
---

# Pengguna, peranan dan kumpulan

## Akaun baharu tidak boleh buat apa-apa {#pending}

Akaun yang baru dicipta — oleh pentadbir, melalui pendaftaran sendiri, atau melalui log masuk
pertama menerusi direktori yang disambungkan — memegang **tiada peranan langsung**. Pemiliknya
melihat skrin *menunggu pelepasan* dan bukan ruang kerja, dan tidak boleh buat apa-apa sehingga
seseorang menetapkan satu.

Itu disengajakan. Alternatifnya, mewarisi tahap akses lalai, bermakna orang asing yang mendaftar
sendiri menjadi pengguna pelayan identiti anda sebaik sahaja mereka menaip alamat e-mel.

Untuk melepaskannya: **Pengguna**, cari akaun itu, tetapkan peranannya, simpan. Skrin orang itu ada
butang **Semak semula**; mereka tidak perlu log keluar dan masuk semula.

> [!NOTE]
> Jika akaun itu langsung tidak sepatutnya wujud, tetapkannya tidak aktif dan bukannya memberikan
> peranan. Memadamnya juga berfungsi, tetapi meninggalkan jejak audit yang menunjuk kepada akaun yang
> tidak boleh dicari sesiapa.

## Peranan menentukan menu, bukan hanya API {#matrix}

Peranan ialah senarai peraturan, satu bagi setiap awalan laluan API, masing-masing memberikan
sebahagian daripada GET, POST, PUT dan DELETE. **Awalan padanan terpanjang menang, dan tiada
peraturan bermakna ditolak.**

Peraturan yang sama membina bar navigasi. Memberikan peranan `GET` pada `/api/user-credential` ialah
yang membuatkan **Pengguna** muncul untuknya; menariknya balik ialah yang membuatkannya hilang. Tiada
konfigurasi menu berasingan yang perlu diselaraskan — itulah sebabnya tiada cara untuk menu
menjanjikan sesuatu yang kemudiannya ditolak oleh pelayan.

Dua akibat:

- **Dua orang pada pelayan yang sama melihat bar berbeza.** Menu yang pendek ialah sistem yang
  berfungsi. Jika rakan sekerja boleh melihat skrin yang anda tidak boleh, bandingkan peranan, bukan
  pelayar.
- **Pemberian yang luas lebih luas daripada rupanya.** `GET` pada `/api` memadankan setiap laluan
  dalam produk. Berikan awalan yang khusus sebaliknya.

**Superadmin** memintas matriks sepenuhnya. Segelintir skrin — Pengguna, Kumpulan, Peranan, RBAC,
Log audit, Sandaran & pemulihan, Tetapan — adalah khusus superadmin tanpa mengira apa kata matriks,
kerana ia boleh digunakan untuk memberi keistimewaan, membaca setiap nama pengguna, atau mengeksport
keseluruhan stor identiti. Itu tidak pernah diwakilkan.

## Kumpulan {#groups}

Kumpulan menyusun pemilikan dan hierarki: siapa milik akaun, dan di bawah induk yang mana. Ia bukan
sistem kebenaran kedua — akses datang daripada peranan. Tugas paling lazim bagi kumpulan ialah
menjadi sasaran yang dipetakan oleh kumpulan direktori, supaya orang yang tiba dari domain anda
mendarat di tempat yang munasabah; lihat [Menyambung direktori](directory#mapping).

## Titik akhir, dan tahap akses {#endpoints}

Skrin **Titik akhir** ialah katalog yang menjadi asas penulisan matriks: setiap laluan API yang
disajikan produk, dengan tahapnya.

- **AuthOnly** — sesi diperlukan, kemudian matriks menentukan. Hampir semuanya.
- **Public** — tiada sesi. Dikhaskan untuk perkara yang mesti berfungsi sebelum sesiapa boleh log
  masuk: titik akhir log masuk itu sendiri, pemeriksaan kesihatan, dan manual ini.
- **DevOnly** — tidak disajikan di luar binaan pembangunan.

Anda jarang perlu menyentuh skrin ini. Ia wujud supaya titik akhir baharu tidak boleh senyap-senyap
dicapai tanpa muncul di suatu tempat yang pentadbir boleh lihat.

## Akaun permulaan sepatutnya bersara {#handover}

Sementara superadmin stok masih aktif, sepanduk menyatakannya di bahagian atas ruang kerja. Keadaan
akhir yang dihasratkan ialah orang sebenar log masuk sebagai diri mereka dan akaun permulaan
ditetapkan tidak aktif.

Sehingga itu, setiap tindakan dalam log audit dikaitkan dengan akaun berkongsi, yang menjadikan
jejak itu jauh kurang berguna daripada sepatutnya — "superadmin menukar peranan pada 02:00" tidak
menamakan sesiapa.

## Ke mana seterusnya {#next}

- [Menyambung direktori](directory) — akaun daripada LDAP atau Active Directory.
- [Log audit](audit-log) — rupa perubahan peranan selepas itu.
- [Sesi dan pengesahan bertingkat](sessions-and-step-up) — menamatkan sesi seseorang sekarang.
