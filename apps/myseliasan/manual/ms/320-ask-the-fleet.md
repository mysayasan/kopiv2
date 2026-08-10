---
title: Tanya armada
category: agent
categoryLabel: Ejen AI
summary: Pembantu berasaskan data anda sendiri dan manual terbina dalam — dan apa yang ia tidak akan buat.
order: 320
---

# Tanya armada

Sembang pada halaman **AI Insight** menjawab dua jenis soalan:

- **Apa yang armada saya lakukan?** — "nod mana yang luar talian minggu ini?", "kenapa amaran
  melonjak malam tadi?"
- **Bagaimana produk ini berfungsi?** — "bagaimana saya menambah kamera?", "apa itu kod tuntutan?"

Kedua-duanya dijawab daripada dua sumber berbeza, dan perbezaan itu penting.

## Dari mana jawapan datang {#grounding}

Soalan armada dijawab daripada **jadual satah kawalan ini sendiri**: nod yang diambil dan keadaannya,
suapan peristiwa, statistik bertetingkap, dan penemuan ringkasan terkini.

Soalan produk dijawab daripada **manual terbina dalam** — manual ini, dan manual MyMataSan, yang
dikompilkan ke dalam satah kawalan ini supaya soalan tentang peranti kamera anda boleh dijawab di
sini.

Tiada apa-apa lain dirujuk, dan **tiada apa-apa keluar dari rangkaian anda**. Tiada perkhidmatan
luar dalam laluan ini, dan itulah yang menjadikan pembantu ini boleh digunakan di tapak terasing
sama sekali.

Pembantu ini diarahkan supaya mengasingkan kedua-duanya: manual menerangkan cara perisian berfungsi
secara umum, dan ia tidak boleh sekali-kali dilaporkan sebagai pemerhatian tentang pemasangan anda.
"Rakaman disimpan selama 14 hari" ialah ayat tentang produk, bukan tentang cakera anda.

## Sumber {#sources}

Apabila jawapan bersandar pada manual, bahagian yang digunakannya muncul di bawah jawapan itu.

**Sumber** ialah bahagian yang benar-benar dipetik oleh jawapan. Apa-apa lagi yang ditawarkan
kepadanya muncul berasingan sebagai **bacaan lanjut** — supaya senarai itu tidak sekali-kali
memberi tanggapan bahawa jawapan bersandar pada halaman yang diabaikannya.

Bahagian daripada manual ini akan terbuka dalam laci bantuan apabila anda mengkliknya. Bahagian
daripada manual produk lain akan menamakan produk itu sebaliknya, kerana satah kawalan ini tidak
boleh memaparkan bantuan peranti lain — tetapi kini anda tahu produk mana dan halaman mana perlu
dibuka.

## Bertanya tentang satu nod {#node-detail}

Namakan sesebuah nod dalam soalan anda dan pembantu turut mengambil **peristiwa terkini nod itu
sendiri** melalui saluran kawalan.

Satu nod, secara ringkas. Ia tidak mencapah ke seluruh armada: itu akan meletakkan tamat masa
setiap nod yang tidak dapat dihubungi ke dalam laluan jawapan anda. Jika nod yang dinamakan berada
di luar talian, jawapan menyatakan ia tidak dapat dihubungi dan bukannya tersekat atau senyap-senyap
meninggalkannya.

## Ia menjawab dalam bahasa anda {#language}

Balasan mengikut bahasa antara muka — Inggeris, Melayu, Cina atau Arab.

Kualiti jawapan dalam sesuatu bahasa bergantung pada model yang anda jalankan. Model kecil mungkin
kembali kepada bahasa Inggeris pada tulisan bukan Latin; model yang lebih besar mengekalkan bahasa
itu dan memetik dengan lebih boleh dipercayai. Jika jawapan Arab atau Cina kembali dalam bahasa
Inggeris, modellah yang perlu ditukar.

## Tanpa model bahasa {#no-model}

Satah kawalan dihantar tanpa sebarang model diaktifkan, dan kotak sembang menyatakannya.

Bahagian manual masih berfungsi. Anda boleh mencari manual terbina dalam daripada kad yang sama dan
mendapat bahagian yang menjawab soalan anda, lengkap dengan pautan yang berfungsi — carian tidak
memerlukan model, muat turun mahupun rangkaian. Ia jawapan yang lebih lemah daripada prosa, dan
jauh lebih baik daripada tiada apa-apa.

## Apa yang ia tidak akan buat {#limits}

- **Ia tidak bertindak.** Ia tidak boleh mengambil nod, mengubah peraturan, mengakui amaran atau
  memulakan semula apa-apa. Ia membaca dan menjawab.
- **Ia tidak menggantikan amaran.** Amaran, peraturan dan ringkasan berjalan sama ada model dipasang
  atau tidak. Tiada apa-apa yang penting menunggu inferens.
- **Ia hanya melihat satu tetingkap.** Soalan armada dijawab merentas tempoh terkini, bukan seluruh
  sejarah.
- **Ia boleh silap.** Ia berasaskan data dan diarahkan menolak apabila data tidak mengandungi
  jawapannya, tetapi model kecil masih boleh tersalah baca apa yang diberikan kepadanya. Id
  peristiwa dan bahagian manual yang dipetik ada di situ supaya anda boleh menyemaknya — untuk
  apa-apa yang berkesan besar, semaklah.

## Apabila ia berkata ia tiada data itu {#no-answer}

Itu biasanya kelakuan yang betul dan bukan kerosakan. Pembantu diarahkan supaya tidak meneka, jadi
soalan di luar tetingkapnya — atau tentang sesuatu yang manual memang tidak liputi — mendapat
penolakan dan bukan rekaan.

Menyusun semula soalan menggunakan perkataan yang dipakai produk lebih membantu daripada bertanya
lagi. Untuk soalan tentang nod tertentu, namakan nod itu.
