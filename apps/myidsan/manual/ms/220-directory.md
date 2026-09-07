---
title: Menyambung direktori
category: people
categoryLabel: Orang dan akses
summary: Log masuk orang dengan akaun LDAP atau Active Directory yang mereka sudah ada.
order: 220
---

# Menyambung direktori

MyIDSan boleh memeriksa kata laluan terhadap pelayan LDAP atau Active Directory yang anda sudah
jalankan, supaya orang log masuk dengan akaun domain yang mereka ada dan bukan akaun kedua yang
perlu diingat. Skrin **Direktori** mengkonfigurasikannya, dan hanya ada satu direktori.

## Apa yang MyIDSan ambil dan tidak ambil daripada direktori {#scope}

Ia mengambil **pengesahan** — adakah ini kata laluan yang betul — dan **keahlian kumpulan**. Ia
tidak menyalin direktori anda ke dalam pangkalan datanya sendiri, dan ia tidak menulis ke direktori
anda. Akaun yang disimpan MyIDSan ialah cangkerang tempatan yang memegang peranan dan sejarah audit;
kelayakan itu kekal di tempat asalnya.

Kata laluan tidak pernah disimpan untuk akaun direktori. Kata laluan *pengguna* juga tidak pernah
dilihat melangkaui ikatan yang memeriksanya.

## Mengisi borang {#form}

- **Hos pelayan** dan **Port**, dengan **StartTLS** dihidupkan untuk port teks biasa yang
  dinaik taraf, dimatikan untuk TLS tersirat (`ldaps`).
- **Sijil CA yang disemat** — tampal PEM jika sijil direktori ditandatangani oleh pihak berkuasa
  anda sendiri. Ini biasanya punca kegagalan sambungan.
- **DN ikatan akaun perkhidmatan** dan **kata laluan** — akaun baca sahaja yang MyIDSan gunakan
  untuk mencari pengguna. Kata laluan itu tulis sahaja: biarkan medan kosong untuk mengekalkan yang
  tersimpan, supaya menyimpan suntingan lain tidak pernah bermakna menaip semula rahsia.
- **DN asas** — tempat carian bermula.
- **Penapis pengguna** — dengan `%s` mewakili nama pengguna yang sedang dicari.
- **Atribut kumpulan** — atribut yang memegang kumpulan pengguna. Pada Active Directory ini
  biasanya `memberOf`.
- **Atribut subjek** — atribut yang mengenal pasti akaun itu **sepanjang hayatnya**. Lihat di
  bawah; inilah satu medan yang berbaloi ditetapkan dengan betul kali pertama.
- **Label pilihan log masuk** — perkataan pada butang di skrin log masuk. "Akaun domain" lebih baik
  daripada "LDAP" bagi orang yang perlu mengkliknya.

**Uji sambungan** berjalan terhadap tetapan yang ada dalam borang sekarang, bukan yang disimpan,
jadi anda boleh membuktikan suntingan sebelum melaksanakannya. Beri ia nama pengguna contoh dan ia
akan melaporkan kumpulan yang ditemuinya — itulah yang anda perlukan sebelum menulis sebarang
pemetaan.

> [!IMPORTANT]
> Pilih atribut subjek sebagai sesuatu yang **tidak boleh berubah** — GUID objek dan bukan alamat
> e-mel atau nama pengguna. Itulah yang dipadankan bagi orang yang kembali, dan memadankan pada
> sesuatu yang boleh diberi nilai baharu bermakna penukaran nama menyerahkan akaun orang lain kepada
> mereka, atau meninggalkan mereka terkandas dengan akaun baharu tanpa peranan.

## Memetakan kumpulan kepada peranan {#mapping}

Kumpulan direktori dengan sendirinya tidak memberikan apa-apa di sini. Tambah pemetaan — DN atau
nama kumpulan, kepada peranan, dengan keutamaan — dan orang dalam kumpulan itu mendapat peranan itu.

- **Keutamaan tertinggi menang** apabila seseorang berada dalam beberapa kumpulan yang dipetakan.
- **Tidak sepadan dengan sebarang pemetaan bukan penolakan.** Orang itu log masuk dengan jayanya dan
  mendarat pada skrin menunggu pelepasan, sama seperti mana-mana akaun baharu. Lihat
  [Pengguna, peranan dan kumpulan](users-roles-groups#pending).

**Direktori berautoriti** menentukan apa yang berlaku pada log masuk *kedua*. Apabila dihidupkan,
pemetaan digunakan semula pada setiap log masuk: peranan yang ditetapkan dengan tangan di sini ditulis
ganti, dan orang yang dikeluarkan daripada kumpulan dalam direktori kehilangan peranan yang
diberikannya pada kali berikutnya mereka log masuk. Apabila dimatikan, pemetaan menyemai peranan
sekali dan suntingan tempatan kekal.

Hidupkannya apabila direktori itulah tempat akses sebenarnya ditentukan — itulah tujuan menyambungkan
satu. Biarkannya mati hanya jika anda berhasrat menguruskan peranan di sini, dan kemudian berterus
terang bahawa mengeluarkan seseorang daripada kumpulan domain tidak akan mengeluarkan akses mereka.

## Faktor kedua untuk akaun direktori {#mfa}

Dasar MFA yang diwajibkan **tidak** meliputi akaun direktori melainkan anda turut menghidupkan
*guna pakai pada direktori*. Lalai itu disengajakan: dasar faktor bagi pengguna tersebut biasanya
milik domain, dan menguatkuasakan satu di sini menduakan sesuatu yang domain sudah minta. Lihat
[Faktor kedua dan pemulihan akaun](second-factor#policy).

## Apabila ia tidak berfungsi {#troubleshooting}

- **Sambungan gagal** — paling kerap sijil. Sama ada semat PEM CA, atau periksa StartTLS sepadan
  dengan port yang anda beri. Uji sambungan melaporkan ralat asas dan bukan kegagalan umum.
- **Ikatan berjaya tetapi tiada kumpulan kembali** — atribut kumpulan salah, atau direktori tidak
  mengisinya bagi akaun itu. Sesetengah pelayan tidak mengisi `memberOf` melainkan kumpulan itu
  sendiri berjenis yang menyelenggaranya.
- **Semua orang mendarat pada menunggu pelepasan** — pemetaan tidak memadankan apa-apa. Jalankan
  ujian dengan nama pengguna contoh dan salin rentetan kumpulan yang benar-benar dipulangkannya.

Setiap perubahan pada skrin ini ditulis ke [log audit](audit-log), termasuk hos, DN asas dan bendera
berautoriti — tidak pernah kata laluan ikatan.

## Ke mana seterusnya {#next}

- [Pengguna, peranan dan kumpulan](users-roles-groups) — apa yang diberikan peranan setelah pemetaan
  digunakan.
- [Log audit](audit-log) — perubahan direktori dan log masuk yang menyusulinya.
