---
title: Faktor kedua dan pemulihan akaun
category: security
categoryLabel: Keselamatan
summary: Kod pengesah, kunci keselamatan, kod pemulihan, dan jalan kembali apabila semuanya hilang.
order: 310
---

# Faktor kedua dan pemulihan akaun

Faktor kedua hanya terpakai kepada kelayakan yang **diperiksa sendiri oleh pelayan ini** — akaun
tempatan. Faktor kedua bagi akaun direktori atau sosial adalah milik pihak yang mengeluarkannya,
melainkan anda menyatakan sebaliknya; lihat [dasar](#policy) di bawah.

Semua pada halaman ini bagi akaun anda sendiri berada di belakang cip di bahagian bawah bar, di
bawah **Profil**.

## Aplikasi pengesah {#enrol}

Pendaftaran menyediakan faktor dan hanya mengaktifkannya setelah anda membuktikan satu kod, jadi kod
QR yang tersalah imbas tidak boleh mengunci anda keluar. Imbas kod itu dengan mana-mana aplikasi
TOTP — Google Authenticator, Aegis, 1Password — atau taip kuncinya secara manual jika kamera menjadi
masalah, kemudian masukkan enam digit yang ditunjukkannya.

Kod ialah enam digit biasa pada langkah tiga puluh saat. Satu langkah di kedua-dua belah *sekarang*
diterima dan tidak lebih, jadi peranti yang jamnya terpesong lebih daripada kira-kira satu minit akan
ditolak setiap kali. Jika kod tiba-tiba berhenti berfungsi dan tiada apa-apa lagi berubah, periksa
jam telefon sebelum apa-apa lagi.

## Kod pemulihan {#recovery}

Pendaftaran mencipta **sepuluh kod pemulihan sekali guna**, ditunjukkan sekali dan tidak pernah lagi.
Setiap satu membawa anda melepasi gesaan faktor kedua tepat sekali dan kemudian habis.

Simpan ia di tempat yang bukan telefon yang memegang pengesah itu. Itulah keseluruhan tujuannya:
peristiwa yang menyebabkan ia wujud ialah peristiwa apabila telefon itu hilang.

Profil anda menunjukkan berapa banyak yang tinggal. Menjana semula menggantikan keseluruhan set —
sepuluh yang lama berhenti berfungsi serta-merta — dan memerlukan kod semasa, jadi seseorang yang
hanya memegang kuki anda tidak boleh mencipta set baharu untuk dirinya.

Menggunakan satu direkodkan dalam [log audit](audit-log) sebagai tindakan tersendiri, berasingan
daripada log masuk biasa. Pembakaran kod pemulihan ialah isyarat terkuat yang ada bahawa seseorang
telah kehilangan pengesah — atau mengambil alihnya — dan meleburkannya menjadi "telah log masuk"
akan menyembunyikan tepat perkara itu.

## Kunci keselamatan {#keys}

Kunci perkakasan atau pengesah terbina (Windows Hello, Touch ID) lebih kuat daripada kod: ia
membuktikan dirinya dengan tandatangan, rahsianya tidak pernah meninggalkan peranti, dan tiada apa
pada pelayan ini untuk dipancing atau disalin. Tambahkannya di bawah **Profil › Kunci keselamatan**.

Dua perkara yang perlu diketahui:

- **Ia memerlukan HTTPS.** Pada alamat `http://` biasa pelayar langsung tidak akan menawarkannya,
  dalam mana-mana pelayar. Itu peraturan pelayar, bukan pelayan ini.
- **Satu akaun boleh memegang beberapa**, dan sepatutnya begitu. Satu kunci ialah satu titik
  kegagalan; kunci ditambah aplikasi pengesah bermakna kehilangan salah satu ialah kesulitan dan
  bukan prosedur pemulihan.

## Mewajibkan faktor {#policy}

Di bawah **Tetapan › Log masuk**, dasar ialah satu daripada tiga:

- **off** — tiada sesiapa digesa. Faktor yang sudah wujud tetap dihormati semasa log masuk.
- **optional** — layan diri sahaja. Ini yang lalai.
- **required** — semua orang dalam skop mesti mendaftar sebelum boleh menggunakan aplikasi.

"Required" boleh dipersempit kepada peranan tertentu, iaitu bentuk yang lazim: wajib bagi pentadbir,
pilihan bagi semua orang lain.

Penguatkuasaan berlaku **selepas** kata laluan berjaya. Seseorang yang terhutang faktor tetap
mendapat sesi dan dipautkan pada skrin pendaftaran sehingga mereka menambah satu — lihat susunan
pintu dalam [Log masuk buat kali pertama](first-sign-in#gates). Itulah yang menjadikan dasar ini
selamat dihidupkan semasa orang sedang menggunakan pelayan: ia membuatkan mereka MENAMBAH faktor,
dan bukan membuktikan satu yang mereka tidak ada.

Akaun direktori **tiada** dalam skop melainkan *guna pakai pada direktori* turut dihidupkan. Lihat
[Menyambung direktori](directory#mfa).

## Apabila pengesah hilang {#lost}

```flow
title : Cara masuk semula apabila pengesah hilang
step lost : Pengesah anda hilang, dipadam atau diganti
ask codes : Adakah anda masih ada salah satu kod pemulihan anda?
ok signin : Log masuk dengannya, kemudian daftarkan pengesah baharu serta-merta
ask other : Adakah ada superadmin lain yang boleh bertindak untuk anda?
step admin : Mereka membersihkan faktor kedua pada akaun anda
ask sole : Adakah akaun yang terkunci itu SATU-SATUNYA superadmin pelayan ini?
step marker => first-sign-in#reset-admin : Letakkan fail RESET_MFA dalam direktori data dan mula semula
end wait : Tanya seorang superadmin — faktor itu tidak boleh dipintas dari skrin log masuk
lost -> codes
codes -> signin : ya
codes -> other : tidak
other -> admin : ya
admin -> signin
other -> sole : tidak
sole -> marker : ya
sole -> wait : tidak
marker -> signin
```

Superadmin yang membersihkan faktor orang lain perlu membuktikan semula kelayakannya sendiri dahulu
— itulah tepat apa yang akan cuba dilakukan oleh penyerang yang memegang kuki curi, jadi ia berada di
belakang [pengesahan bertingkat](sessions-and-step-up#stepup). Hari ini pembersihan itu ialah
panggilan API dan bukan butang pada skrin Pengguna.

Penanda `RESET_MFA` membersihkan faktor **superadmin permulaan** dan tiada apa-apa lagi — bukan kata
laluan, bukan faktor orang lain. Ia digunakan dan dipadamkan pada permulaan berikutnya sebelum ia
bertindak, dan direkodkan dalam log audit tanpa pelaku, kerana tiada sesiapa log masuk untuk
menyebabkannya. Sesiapa yang meletakkan fail itu mempunyai akses tulis kepada direktori data, dan
itulah yang ditunjukkan oleh catatan tersebut.

## Kata laluan yang terlupa {#password-recovery}

Pautan **Lupa kata laluan** pada skrin log masuk berfungsi untuk akaun tempatan dan sentiasa
memberitahu orang itu perkara yang sama, sama ada akaun itu wujud atau tidak. Pelayan identiti yang
berkata "tiada pengguna sedemikian" ialah senarai direktori untuk sesiapa yang mahukannya.

Di belakang itu, salah satu daripada dua perkara berlaku:

- **Barisan giliran operator.** Permintaan tertunggak muncul di bawah **Permintaan set semula**.
  Superadmin menyelesaikannya dengan mengeluarkan kata laluan sementara yang baharu, ditandakan
  mesti-tukar, dan menyerahkannya melalui saluran yang mereka percayai. Laluan ini sentiasa
  berfungsi, termasuk tanpa sebarang pelayan mel — iaitu keadaan biasa pada pemasangan bersela udara.
- **Pautan layan diri**, hanya apabila geganti SMTP dalaman telah dikonfigurasikan. Pautan itu hidup
  setengah jam dan menetapkan kata laluan untuk satu akaun, sekali.

Akaun direktori dan sosial tidak dikendalikan di sini; kata laluan mereka milik penyedia mereka, dan
skrin itu menyatakannya dan bukannya berpura-pura.

## Ke mana seterusnya {#next}

- [Sesi dan pengesahan bertingkat](sessions-and-step-up) — apa yang dilindungi oleh pengesahan semula.
- [Log audit](audit-log) — setiap perubahan faktor dan setiap pembakaran kod pemulihan.
