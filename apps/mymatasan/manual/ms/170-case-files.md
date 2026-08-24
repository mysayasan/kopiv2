---
title: Fail kes
category: daily-use
categoryLabel: Penggunaan harian
summary: Kumpulkan rakaman, penampakan dan nota bagi satu insiden di satu tempat — dan pastikan rakaman itu selamat daripada dasar pengekalan sehingga anda selesai dengannya.
order: 170
---

# Fail kes

Segala yang dirakam oleh peranti ini disusun mengikut kamera dan mengikut masa. Satu insiden
bukan kedua-duanya. Ia adalah seorang yang melintasi empat kamera dalam sebelas minit,
amaran yang berbunyi di pertengahannya, dan dua perkara yang anda perasan selepas itu.

Satu **kes** ialah bekas untuk semua itu. Ia menyimpan klip, penampakan dan nota anda; ia
boleh diserahkan kepada rakan sekerja; ia ditutup dengan keputusan yang dinyatakan; dan ia
dieksport sebagai satu fail boleh sahih yang boleh anda berikan kepada orang lain.

Ia juga melakukan satu perkara yang tidak mampu dilakukan oleh folder klip yang dimuat
turun: **ia mengekalkan rakamannya.**

## Membuka kes {#opening}

**Kes → Kes baharu**, dan berikan tajuk. Itu sahaja upacaranya — kes yang bertajuk boleh
diisi sambil anda bekerja.

Tugaskan kepada diri sendiri atau rakan sekerja melalui senarai **Ditugaskan kepada** pada
kes itu. Penugasan direkodkan dalam jejak audit, jadi "siapa yang menguruskan ini" ada
jawapannya kemudian.

## Menambah bukti {#adding}

Bukti ditambah dari tempat anda menemuinya, bukan ditaip ke dalam kes secara manual:

- **Garis Masa → Tambah ke kes** menandakan detik yang sedang anda tonton, pada setiap
  kamera di skrin. Itulah rupa sesuatu insiden lazimnya: satu detik, beberapa sudut.
- **Objek → Tambah ke kes** menambah satu penampakan yang dirakam.
- **Tambah nota**, di dalam kes, merekodkan sesuatu yang tiada rakaman di sebaliknya —
  nombor pendaftaran, kata-kata saksi, atau kesimpulan anda.

Setiap bukti membawa nota. Gunakannya. Kes dengan enam klip tanpa nota memberitahu orang
seterusnya apa yang dirakam, bukan apa maknanya.

> [!NOTE]
> Menambah bukti menyalin *masa dan kamera*, bukan videonya. Video kekal di tempatnya —
> sebab itulah bahagian seterusnya penting.

## Rakaman dalam kes tidak dipadam {#holding}

Selagi kes **terbuka**, rakaman yang dirujuk oleh buktinya akan disimpan: dasar pengekalan
tidak akan memadamkannya, **Padam sekarang** bagi kamera itu tidak akan memadamkannya, dan
begitu juga pembersihan automatik yang berjalan apabila cakera menjadi penuh.

Inilah sebab untuk membuka kes. Dengan pengekalan tujuh hari, kes yang dibuka hari ini akan
menjadi senarai pautan rosak minggu hadapan.

Kes menyatakan apa yang disimpannya, di bahagian atas: berapa banyak klip, berapa banyak
ruang cakera, dan — angka yang paling patut dibaca — **berapa banyak daripadanya masih ada
semata-mata kerana kes ini terbuka**. Itulah klip yang sudah melepasi tarikh pengekalannya.

> [!NOTE]
> **Pemadaman selamat**, **tetapan semula kilang**, dan **memadam kamera** tetap
> memusnahkan rakaman tanpa mengira mana-mana kes. Itulah operasi "musnahkan segalanya" dan
> satu kes bukan kunci terhadapnya.

Membuang satu bukti, atau menutup kes, melepaskan rakaman itu kembali kepada dasar
pengekalan biasa. Tiada apa-apa dipadam pada saat itu — rakaman itu sekadar kembali kepada
jangka hayat yang sepatutnya jika kes itu tidak pernah wujud, dan pembersihan seterusnya
dikenakan padanya seperti mana-mana yang lain.

## Mengeksport kes {#exporting}

**Bina bungkusan**, dengan satu sebab, menghasilkan satu `.zip` yang mengandungi:

- `clips/` — setiap rakaman dalam kes, dicantum tanpa dienkod semula.
- `manifest.json` — apa setiap klip itu, segmen rakaman asalnya, SHA-256 bagi setiap satu,
  dan mana-mana tempoh di dalam klip yang tiada rakaman.
- `chain-of-custody.csv` — setiap tindakan yang direkodkan pada kes ini, mengikut turutan:
  siapa yang membukanya, siapa menambah apa, siapa membuat anotasi, siapa mengeksport.
- `VERIFY.txt` — cara menyemak semua di atas dengan alat biasa.

Sebab adalah wajib, dan eksport ditulis ke dalam jejak audit dua kali: sekali ketika ia
diminta dan sekali ketika fail itu benar-benar dimuat turun.

Jika sesuatu bukti tiada lagi rakaman, bungkusan **tetap dihasilkan** dan klip itu
disenaraikan sebagai hilang, berserta sebabnya — pada skrin dan di dalam fail. Eksport yang
diam-diam meninggalkannya akan kelihatan lengkap.

Eksport tersedia kepada operator dan juga pentadbir. Memadam rakaman tidak — lihat
[Pengguna dan peranan](users-and-roles).

## Menutup kes {#closing}

**Tutup kes** meminta satu keputusan, dan mewajibkannya. Kes yang ditutup tanpa keputusan
yang dinyatakan tidak dapat dibezakan daripada kes yang sekadar dikemaskan oleh seseorang.

Dialog itu menyatakan semula apa yang dilepaskan oleh penutupan, termasuk berapa banyak klip
yang sudah melepasi tarikh pengekalan dan akan hilang pada pembersihan seterusnya. **Jika
anda memerlukannya, eksport kes itu dahulu.**

Kes yang ditutup boleh dibuka semula, dan ia akan menyimpan rakamannya sekali lagi — bagi
apa-apa yang masih ada.
