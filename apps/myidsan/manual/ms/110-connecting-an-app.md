---
title: Menyambung aplikasi
category: federation
categoryLabel: Menyambung aplikasi
summary: Daftarkan aplikasi bergantung, berikannya rahsia, dan fahami lompatan yang dibuatnya.
order: 110
---

# Menyambung aplikasi

Aplikasi yang melog masuk orang melalui MyIDSan ialah **aplikasi bergantung**. Mendaftarkan satu
dilakukan pada skrin **Aplikasi**, dan memerlukan empat perkara: kod, khalayak, URL asas, dan
sekurang-kurangnya satu URI ubah hala.

## Apa yang sebenarnya berlaku {#flow}

Tiada apa-apa di sini yang dicipta pada waktu log masuk. Aplikasi menghantar pelayar orang itu ke
MyIDSan dengan nilai yang anda daftarkan; MyIDSan menjalankan pintu-pintunya sendiri dan menghantar
pelayar kembali dengan kod sekali guna; **pelayan** aplikasi itu menukar kod tersebut dengan
identiti orang itu menggunakan rahsia yang tidak pernah dilihat oleh pelayar.

```seq
title : Satu log masuk, dari sudut aplikasi bergantung
actor app : Aplikasi anda
actor browser : Pelayar orang itu
actor idsan => first-sign-in#gates : MyIDSan
app -> browser : Tiada sesiapa log masuk di sini — pergi dan tanya
browser -> idsan : Minta kebenaran, menyebut id klien, khalayak, URI ubah hala dan state yang anda jana
idsan -> browser : Skrin log masuk, dan setiap pintu di belakangnya
browser -> app : Kembali ke URI ubah hala anda, membawa kod sekali guna dan state anda semula
app -> idsan : Tukarkan kod itu, menyebut rahsia klien anda
idsan --> app : Siapa mereka, peranan yang mereka pegang, dan untuk berapa lama
```

Dua akibat yang wajar dinyatakan dengan jelas:

- **Rahsia digunakan pelayan ke pelayan.** Jika aplikasi anda ialah aplikasi satu halaman tanpa
  bahagian belakangnya sendiri, ia tiada tempat selamat untuk menyimpan rahsia dan tidak boleh
  menyelesaikan pertukaran ini.
- **Kod itu sekali guna.** Pertukaran kedua bagi kod yang sama ditolak dan direkodkan sebagai
  penolakan dalam [log audit](audit-log). Itulah rupa kod yang dimain semula, dan ia memang
  sepatutnya kelihatan.

## Mengisi borang {#form}

- **Kod** — pengenal ringkas untuk aplikasi: huruf kecil, digit dan sengkang, bermula dengan huruf.
  Ia pemegang tetap aplikasi dan tidak boleh ditukar kemudian.
- **Khalayak** — `aud` yang dicap pada token yang dikeluarkan. Kelaziman dalam suite ini ialah ia
  sepadan dengan kod, dan borang mengekalkannya seiring sehingga anda mengeditnya. Khalayak yang
  berbeza daripada kod adalah sah; ia cuma perlu dinyatakan dengan sengaja.
- **URL asas** — tempat aplikasi itu berada. `http://` diterima hanya untuk `localhost`; apa-apa
  lain melalui HTTP biasa ditandakan, kerana ubah hala itu membawa kod kebenaran.
- **URI ubah hala** — tempat MyIDSan menghantar pelayar kembali. **Ia dipadankan dengan tepat**:
  skema, hos, port, laluan, sengkang di hujung dan semuanya. `https://app.example.com/api/auth/callback`
  yang didaftarkan tidak sepadan dengan `https://app.example.com/api/auth/callback/`. Aplikasi suite
  ini sendiri melekapkannya di `/api/auth/callback`.

Bahagian kanan skrin menunjukkan **URL kebenaran langsung** dan petikan konfigurasi yang dibina
daripada apa yang anda taip setakat ini. Baca yang itu dan bukan halaman ini: ia dijana daripada
alamat pelayan ini sendiri dan nilai sebenar anda, jadi ia tidak boleh menjadi lapuk.

## Rahsia klien {#secret}

Jana rahsia pada skrin Aplikasi. MyIDSan hanya menyimpan cincangannya — API tidak pernah
memulangkannya, dan ia wujud dalam teks biasa **hanya dalam tab pelayar yang menjananya**.

Itu ada satu akibat praktikal: salin ia, atau eksport berkasnya, sebelum anda meninggalkan halaman.
Jika anda kehilangannya, anda memutarnya dan bukan memulihkannya, dan aplikasi itu berhenti melog
masuk sesiapa sehingga ia dikonfigurasikan semula dengan yang baharu. Putaran direkodkan dalam log
audit.

## Menyerahkan nilai {#export}

Daripada menaip semula id klien dan khalayak merentasi dua konsol, gunakan **Eksport** pada skrin
Aplikasi. Ia menulis fail JSON kecil yang menyimpan pengeluar, khalayak, URL asas penyedia, id
klien, asas dan laluan ubah hala, serta hayat sesi — dan rahsia juga, tetapi hanya apabila satu baru
sahaja dijana dalam tab yang sama itu. Fail itu menyatakan yang mana satu daripada dua itu dan
bukannya menghantar berkas yang senyap-senyap tidak akan berfungsi.

MySeliaSan mengimport fail itu terus pada skrin Tetapannya: ia mengisi borang dan menunggu anda
menyimpan. Dua konsol, satu sumber kebenaran.

## Hayat setiap bahagian {#lifetimes}

```spec
title : Berapa lama setiap bahagian pertukaran ini hidup, sebagaimana ia dihantar
row code `300s` : Kod kebenaran. Ia hanya perlu bertahan sepanjang ubah hala kembali dan pertukaran segera oleh aplikasi anda, jadi ia sengaja dibuat pendek.
row token `900s` : Token akses yang diserahkan kepada aplikasi anda.
row session `259200s` : Log masuk di sini — tiga hari. Inilah sebabnya berpindah antara aplikasi tidak meminta kata laluan semula, dan inilah nombor yang menentukan berapa lama kuki sesi yang dicuri masih bernilai.
```

Setiap satu boleh diatasi bagi setiap aplikasi pada skrin Aplikasi, iaitu tempat yang betul untuk
memendekkannya bagi satu aplikasi sensitif tanpa memendekkannya untuk semua orang.

> [!NOTE]
> Sesi tiga hari itu juga sebabnya
> [pengesahan semula bertingkat](sessions-and-step-up#stepup) wujud. Kuki yang diambil daripada
> komputer riba yang tidak berkunci bernilai selama tiga hari; pengesahan bertingkat ialah yang
> menghalangnya daripada bernilai untuk perubahan peranan dan eksport identiti.

## Apabila ia tidak berfungsi {#troubleshooting}

Hampir setiap kegagalan ialah salah satu daripada empat, dan log audit menyatakan yang mana:

- **`redirect_uri is not registered`** — kegagalan padanan tepat. Bandingkan dua rentetan itu aksara
  demi aksara, termasuk sengkang di hujung.
- **Klien tidak dikenali** — kod atau id klien tidak sepadan dengan aplikasi berdaftar yang aktif.
  Periksa aplikasi itu tidak ditetapkan tidak aktif.
- **Rahsia itu salah** — biasanya putaran yang hanya sampai ke satu pihak.
- **Kod itu sudah digunakan** — sama ada main semula sebenar, atau aplikasi anda mencuba semula
  pertukaran selepas tamat masa. Ulang seluruh lompatan kebenaran, bukan pertukaran.

Semua dalam senarai itu ditulis ke [log audit](audit-log#federation) sebagai penolakan, dengan
aplikasi yang ia ditolak untuknya. Itulah tempat pertama untuk dilihat — aplikasi anda hanya melihat
ralatnya sendiri.

## Ke mana seterusnya {#next}

- [Pengguna, peranan dan kumpulan](users-roles-groups) — peranan yang diterima aplikasi anda.
- [Log audit](audit-log) — siapa dibenarkan masuk ke aplikasi yang mana.
