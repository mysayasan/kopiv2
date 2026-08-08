---
title: Log masuk buat kali pertama
category: getting-started
categoryLabel: Permulaan
summary: Cari kata laluan pentadbir sekali guna, tetapkan kata laluan anda sendiri, dan apa yang perlu dibuat apabila dikunci keluar.
order: 20
---

# Log masuk buat kali pertama

## Mencari kata laluan sekali guna {#first-password}

Kali pertama MyMataSan dimulakan, ia mencipta satu akaun pentadbir dan satu **kata laluan sekali
guna yang dijana khusus untuk pemasangan ini**. Tiada kata laluan lalai yang boleh dicari, dan tiada
dua pemasangan berkongsi kata laluan yang sama.

Nama penggunanya ialah `admin`. Kata laluan diletakkan di dua tempat, supaya anda boleh menemuinya
mengikut cara anda menjalankan peranti ini:

- **Pada konsol.** Sepanduk dicetak semasa permulaan dengan alamat untuk dibuka, nama pengguna dan
  kata laluan. Dalam Docker ia berada dalam `docker logs`; pada Linux ia berada dalam jurnal
  perkhidmatan.
- **Dalam fail.** `INITIAL_ADMIN_LOGIN.txt` ditulis ke dalam direktori data. Gunakan yang ini jika
  konsol sudah berlalu atau perkhidmatan berjalan tanpa tetingkap yang kelihatan — yang merupakan
  keadaan biasa pada Windows.

> [!NOTE]
> Jika anda menetapkan `localAuth` dalam `config.json`, atau pemboleh ubah persekitaran
> `LOCAL_ADMIN_PASSWORD`, sebelum permulaan pertama, kata laluan itu digunakan sebaliknya dan
> **tidak** dipaparkan di mana-mana. Sepanduk itu menunjuk kepada konfigurasi anda dan bukannya
> mencetak rahsia yang sudah anda miliki.

Padamkan `INITIAL_ADMIN_LOGIN.txt` sebaik sahaja anda log masuk dan menetapkan kata laluan sendiri.

## Menetapkan kata laluan anda sendiri {#change-password}

Akaun permulaan ditanda *mesti tukar kata laluan*, jadi perkara pertama yang anda lihat selepas log
masuk ialah skrin tukar kata laluan. Ini bukan cadangan yang boleh diketepikan: sehingga kata laluan
ditukar, akaun itu tidak boleh melakukan apa-apa selain membaca sesinya sendiri dan menukar kata
laluannya sendiri. Setiap permintaan lain ditolak.

Masukkan kata laluan sekali guna sebagai **Kata laluan semasa**, kemudian kata laluan baharu anda
dua kali. Minimumnya lapan aksara; frasa laluan beberapa perkataan yang tidak berkaitan adalah lebih
kuat dan lebih mudah ditaip pada papan kekunci bilik kawalan berbanding gabungan pendek yang rumit.

Sebaik sahaja ia diterima, anda log masuk sepenuhnya dan bestari permulaan bermula — lihat
[Bestari persediaan kali pertama](setup-wizard).

Jika orang lain yang sepatutnya memiliki peranti ini, gunakan **Log masuk sebagai pengguna lain**
pada skrin itu dan bukannya menetapkan kata laluan yang akan anda serahkan.

## Log masuk harian {#daily}

Skrin log masuk menerima nama pengguna dan kata laluan, dan tiada apa-apa lagi. Dua kawalan berada
di sekelilingnya:

- **Penukar bahasa**, di sudut atas. Ia berkuat kuasa serta-merta dan diingati pada pelayar ini,
  jadi terminal yang dikongsi boleh dibiarkan dalam bahasa yang dibaca oleh orang yang
  menggunakannya.
- **Pautan bantuan**, yang membuka manual ini. Ia berfungsi sebelum anda log masuk, iaitu waktu
  anda paling mungkin memerlukannya.

## Apabila log masuk dikunci {#lockout}

Selepas beberapa percubaan gagal daripada alamat yang sama, alamat itu dikunci untuk tempoh yang
berganda dengan setiap kegagalan berikutnya. Skrin log masuk menunjukkan kiraan detik; tiada apa
yang boleh dibuat selain menunggu sehingga ia mencapai sifar. Secara lalai ini bermula selepas **5
percubaan gagal dalam 5 minit**, bermula pada kuncian **1 minit** dan meningkat sehingga paling
lama **15 minit**.

Dua perkara yang perlu diketahui:

- Hanya log masuk interaktif dikira terhadap kuncian. Tab pelayar yang mengulang kelayakan lama di
  latar belakang akan ditolak tetapi tidak menggunakan bajet anda, jadi ia tidak boleh mengunci
  rakan sekerja yang sedang menaip kata laluan yang betul.
- Kuncian menimbulkan pemberitahuan **Kritikal**. Jika kuncian muncul dalam suapan yang tiada
  siapa boleh jelaskan, anggap ia sebagai seseorang sedang menduga peranti ini, bukan sekadar
  gangguan.

Pentadbir tidak boleh memendekkan kuncian orang lain daripada antara muka — kiraan detik itu memang
sengaja bukan sesuatu yang boleh dipujuk oleh penyerang. Tunggu sehingga tamat.

## Apabila peranti meminta kunci pemulihan {#recovery-gate}

Jika MyMataSan bermula dan mendapati penyulitan semasa rehat dihidupkan, bahawa kunci induk pernah
wujud pada mesin ini sebelum ini, dan bahawa ia tidak dapat membaca kunci itu sekarang, ia tidak
akan bermula seperti biasa. Bukannya skrin log masuk, anda akan mendapat skrin **pemulihan** yang
meminta fail kunci yang anda eksport beserta frasa laluannya.

Ini ialah sifat keselamatan, bukan kerosakan: rakaman dan gambar petikan pada cakera ialah teks
sifer, dan tanpa kunci itu ia tidak boleh dibaca. Bermula seperti biasa bermakna menyajikan peranti
yang seolah-olah tiada sejarah.

Muat naik fail `.atrestkey` yang anda eksport semasa penyulitan disediakan, masukkan frasa laluannya,
dan peranti akan dibuka kunci dan dimulakan semula. Jika anda tidak mempunyai fail itu, bahan yang
disulitkan tidak boleh dipulihkan oleh sesiapa pun, termasuk anda — dan itulah tujuan menyulitkannya.

## Ke mana selepas ini {#next}

- [Bestari persediaan kali pertama](setup-wizard) — apa yang dilakukan oleh sembilan langkah itu.
- [Lawatan ruang kerja](workspace-tour) — apa yang anda lihat setelah berada di dalam.
