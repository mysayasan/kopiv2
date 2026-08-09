---
title: Penyulitan semasa rehat dan kunci pemulihan
category: administration
categoryLabel: Pentadbiran
summary: Rakaman disulitkan pada cakera — eksport kunci pemulihan, atau mesin yang mati membawa rakamannya bersamanya.
order: 530
---

# Penyulitan semasa rehat dan kunci pemulihan

Rakaman, gambar petikan dan imej latihan **disulitkan pada cakera secara lalai**. Ia telus: tiada apa
yang perlu dilakukan semasa merakam, main balik atau mengeksport.

Dua akibat menjadikan ini topik pentadbiran dan bukan nota kaki.

## Mengapa ia wujud {#why}

Perakam memegang rakaman orang yang tidak memilih untuk dirakam. Jika mesin itu dicuri, atau sesuatu
cakera diganti di bawah waranti dan meninggalkan tapak, penyulitanlah yang menghalang rakaman itu
daripada boleh dibaca oleh sesiapa yang akhirnya memilikinya.

Ia juga menjadikan set semula kilang **terjamin**. Memusnahkan kunci — *pemadaman kripto* —
menjadikan setiap rakaman serta-merta tidak boleh dipulihkan tanpa mengira saiz atau medium storan,
sesuatu yang penulisan ganti biasa tidak boleh janjikan pada pemacu SSD dan NVMe, di mana pengawal
mungkin menyimpan salinan yang tidak boleh anda alamatkan. Lihat
[Padam selamat dan set semula kilang](secure-wipe-and-reset).

## Eksport kunci pemulihan. Hari ini. {#export}

Kunci induk berada pada mesin ini. Bergantung pada cara ia dilindungi, ia mungkin **terikat kepada
hos ini** — dilindungi oleh simpanan kunci platform supaya ia tidak boleh dibuka pada perkakasan
lain. Tab Sandaran & Pemulihan memberi amaran apabila itu berlaku, dan amaran itu bukan hiasan:

> [!WARNING]
> Jika kunci itu terikat hos dan mesin itu mati atau diimej semula, **setiap rakaman menjadi tidak
> boleh dibaca selama-lamanya.** Bukan sukar — mustahil. Tiada sesiapa boleh memulihkannya, termasuk
> orang yang menulis perisian ini.

**Tetapan → Sandaran & Pemulihan → Eksport kunci pemulihan** memuat turun salinan kunci induk yang
disulitkan dengan frasa laluan. Fail itu tidak pernah mengandungi kunci mentah secara jelas; hanya
frasa laluan yang membukanya.

Lakukan ini pada hari anda mentauliahkan peranti, bukan pada hari anda memerlukannya.

## Menyimpannya {#storing}

- **Frasa laluan ialah satu-satunya yang melindungi fail itu.** Pilihnya sewajarnya.
- **Simpan fail dan frasa laluan berasingan**, dan kedua-duanya di luar talian. Kunci pemulihan yang
  terletak di sebelah frasa laluannya pada peranti yang sama tidak melindungi apa-apa.
- Peti besi, sampul bermeterai, atau proses eskrow kunci sedia ada organisasi anda semuanya sesuai.
  Laci di bawah peranti itu tidak.

## Sahkan {#verify}

**Sahkan kunci pemulihan** ialah semakan baca sahaja: ia mengesahkan fail yang disimpan boleh dibuka
dengan frasa laluannya *dan* bahawa ia melindungi kunci yang sedang digunakan. Ia tidak memulihkan
apa-apa.

Jalankannya sekarang dan selepas sebarang perubahan kunci. Terdapat tiga hasil, dan yang ketiga wajar
ditangkap:

- **Sah, dan sepadan dengan kunci yang digunakan** — anda dilindungi.
- **Frasa laluan salah, atau bukan kunci pemulihan yang sah** — fail atau frasa laluan itu bukan yang
  anda sangka.
- **Frasa laluan betul, tetapi ia melindungi kunci yang *berbeza*** — eksport lama. Anda memegang
  kunci pemulihan bagi keadaan mesin yang tidak lagi wujud, dan ia tidak akan membuka rakaman hari
  ini.

Kes ketiga itu ialah tepat cara orang mendapati, pada detik paling teruk, bahawa eskrow mereka sudah
basi.

## Memulihkan pada perkakasan baharu {#restore}

Prosedur yang direkodkan, yang juga ditunjukkan pada tab:

1. Pasang aplikasi pada mesin baharu.
2. Salin rakaman ke sana.
3. Letakkan fail pemulihan di sebelah kunci sebagai `recovery.atrestkey` (atau halakan
   `security.recoveryPath` kepadanya).
4. Berikan frasa laluannya melalui `security.passphraseFile` atau pemboleh ubah persekitaran
   `ATREST_PASSPHRASE`.

Pada permulaan pertama kunci dipulihkan secara automatik dan rakaman dinyahsulitkan.

Perhatikan apa ini **bukan**: ia bukan pemulihan konfigurasi. Kamera, peraturan dan tetapan datang
daripada [sandaran konfigurasi](backup-and-restore), iaitu fail berasingan dengan frasa laluan
berasingan. Pembinaan semula penuh memerlukan kedua-duanya.

## Skrin pemulihan pada log masuk {#recovery-gate}

Jika peranti dimulakan, mendapati penyulitan dihidupkan dan kunci yang pernah wujud di sini tetapi
tidak boleh dibaca sekarang, ia enggan bermula seperti biasa dan menunjukkan skrin pemulihan dan
bukan skrin log masuk.

Itu sifat keselamatan yang berfungsi: bermula seperti biasa akan menyajikan peranti yang kelihatan
tiada sejarah. Muat naik kunci yang dieksport dan frasa laluannya untuk membuka kunci. Lihat
[Log masuk buat kali pertama](first-sign-in#recovery-gate).

## Mematikannya {#disabling}

Penyulitan boleh dilumpuhkan dalam konfigurasi. Fail teks biasa sedia ada sentiasa dibaca secara
telus, jadi tetapan itu boleh ditukar tanpa migrasi.

Matikannya hanya di mana anda mempunyai sebab tertentu — mesin dalam bilik terkawal yang cakeranya
tidak pernah keluar, menyuap aliran kerja yang tidak dapat menampungnya. Anda kehilangan perlindungan
kecurian dan pemadaman kripto yang terjamin, dan anda mendapat sedikit CPU.

## Apa yang tidak disulitkan {#not-encrypted}

- **Pemberat model** (fail `.pt`) — pekerja pengesanan membacanya secara terus.
- **Klip yang dieksport** — muat turun ialah fail video biasa.
- **Sandaran konfigurasi** — disulitkan, tetapi dengan *frasa laluan anda*, bukan kunci ini. Itu
  disengajakan: itulah sebabnya sandaran boleh dibuka pada mesin lain.
