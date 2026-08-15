---
title: Peraturan armada
category: fleet
categoryLabel: Armada
summary: Kaitkan peristiwa merentas nod berlainan, dan gunakan ketiadaan untuk menyatakan tiada salah.
order: 140
---

# Peraturan armada

Peraturan armada memerhati peristiwa merentas **nod yang berlainan serentak** dan hanya menyala
apabila semuanya sejajar.

Tiada satu nod pun boleh melakukan ini. Nod kamera tidak nampak sesentuh pintu anda; nod penderia
tidak nampak kamera anda. Hanya satah kawalan, yang sudah menerima peristiwa setiap nod dalam satu
suapan, berada pada kedudukan untuk perasan gabungan itu.

## Mengapa korelasi mengatasi mana-mana penderia tunggal {#why}

Dengan sendirinya, amaran gerakan kamera pada 03:00 ialah hingar — kupu-kupu, labah-labah, cahaya
lampu kereta menembusi tingkap. Dengan sendirinya, sesentuh pintu pada 03:00 juga hingar — tukang
cuci, penghantaran, angin.

Kedua-duanya bersama, **tanpa sebarang leretan lencana**, bukan hingar.

Itulah keseluruhan ideanya: korelasi ialah cara sekumpulan penderia yang bising secara individu
menjadi satu isyarat yang boleh dipercayai. Satu peraturan yang benar-benar boleh anda biarkan
hidup lebih bernilai daripada lima yang anda senyapkan.

## Syarat: apa yang mesti berlaku, dan apa yang tidak {#conditions}

Peraturan ialah senarai perkara yang mesti berlaku — dan, yang paling penting, perkara yang mesti
**tidak** berlaku.

Setiap syarat dipadankan pada:

| Medan | Fungsinya |
|---|---|
| **Node type** | Nod kamera, nod penderia, pengawal pintu, atau mana-mana. |
| **Node** | Satu nod tertentu, atau mana-mana. |
| **Category** | Kategori peristiwa, cth. `vision.alert`. |
| **Text to match** | Dipadankan tanpa mengira huruf besar/kecil pada tajuk dan isi peristiwa. |

Teks itu dipadankan dengan **nama peraturan yang menyala pada nod** — "Person detected", "Front
door opened". Nama peraturan nod anda sendiri ialah perbendaharaan kata anda, jadi anda tidak perlu
mencipta taksonomi sebelum boleh menulis peraturan.

Peraturan memerlukan sekurang-kurangnya satu perkara yang mesti berlaku. Peraturan yang dibina
daripada ketiadaan sahaja tidak akan menyala untuk apa-apa pun.

## Ketiadaan ialah cara peraturan menyatakan tiada salah {#absence}

Inilah bahagian ciri ini yang benar-benar berbaloi, dan bahagian yang mudah dilangkau.

Tanpa ketiadaan, "pintu terbuka pada 03:00" menjadi amaran setiap malam tukang cuci bekerja lewat.
Dengan ketiadaan, peraturan menyatakan maksud sebenarnya: *pintu terbuka, kamera nampak gerakan,
dan tiada sesiapa melerat lencana.*

Peristiwa "must NOT have happened" yang padan akan **melucutkan** peraturan, bukan menyalakannya.

## Tetingkap, tempoh ihsan dan tempoh sejuk {#timing}

Tiga nombor, setiap satu menjawab soalan berbeza.

**Window (seconds)** — sedekat mana peristiwa yang diperlukan mesti berlaku untuk dikira sebagai
satu insiden. Itulah bezanya antara "pintu terbuka, dan secara berasingan kamera nampak gerakan
Selasa lalu" dengan satu peristiwa tunggal.

**Grace delay (seconds)** — berapa lama menunggu sebelum mempercayai sesuatu ketiadaan. Apabila
semua peristiwa yang diperlukan telah tiba, peraturan tidak menyala; ia **bersedia**, menunggu
tempoh ihsan ini habis, dan barulah memutuskan sama ada ketiadaan itu benar-benar bertahan.

Tunggu ini bukan hiasan. Pembaca lencana lazimnya satu dua saat *lewat* daripada sesentuh pintu
yang baru sahaja dibenarkannya, jadi leretan yang tiba dalam tempoh ihsan akan melucutkan peraturan
— itu kemasukan yang dibenarkan. Tanpa tunggu ini, peraturan akan menjerit pencerobohan pada setiap
kemasukan lencana yang sah, sepanjang hari, sehingga seseorang mematikannya. Jika dibiar pada 0,
peraturan menunggu 5 saat.

**Cooldown (seconds)** — berapa lama peraturan berdiam selepas menyala, supaya satu insiden menjadi
satu amaran dan bukan seratus. Ia kekal walaupun selepas mula semula.

## Siapa yang boleh mengubahnya {#permissions}

Peraturan armada **ditulis oleh superadmin sahaja**. Peranan lain yang boleh sampai ke halaman ini
membacanya tetapi tidak boleh mengubahnya — peraturan inilah yang menentukan apa yang dianggap
insiden oleh seluruh armada, jadi ia bukan suntingan harian biasa.

Setiap perubahan direkodkan dalam log audit.

## Apabila peraturan tidak pernah menyala {#troubleshooting}

Mengikut susunan kebarangkalian:

1. **Teks tidak padan.** Ia dipadankan dengan nama peraturan nod itu sendiri. Semak perkataan tepat
   dalam suapan pemberitahuan dan bukan menaip apa yang anda sangka tertulis.
2. **Tetingkap terlalu pendek** untuk peristiwa yang memang tiba beberapa saat berselang.
3. **Sesuatu ketiadaan sedang melucutkannya.** Jika pembaca lencana melapor lebih lewat daripada
   sangkaan anda, leretan itu tiba dalam tempoh ihsan — itu kelakuan yang betul, dan peraturan
   sedang memberitahu anda kemasukan itu dibenarkan.
4. **Jenis nod salah.** Gerakan milik nod kamera; sesentuh pintu atau pembaca lencana milik nod
   penderia. Kedua-duanya tidak boleh ditukar ganti.
5. **Peraturan dimatikan**, atau masih dalam tempoh sejuk daripada penyalaan sebelumnya.

## Apabila peraturan menyala terlalu kerap {#noise}

Tambah ketiadaan sebelum anda menaikkan ambang. "Gerakan di ruang muatan" menyala sepanjang hari;
"gerakan di ruang muatan tanpa leretan lencana dan tanpa penghantaran berjadual" menyala apabila
ada sesuatu yang tidak kena.

Menaikkan tempoh sejuk memampatkan ribut menjadi satu amaran, tetapi ia tidak menjadikan peraturan
yang salah itu betul.
