---
title: Bekerja di dalam nod
category: fleet
categoryLabel: Armada
summary: Skrin nod itu sendiri, dibuka dari sini melalui terowong — dan siapa yang menentukan apa yang boleh anda buat.
order: 150
---

# Bekerja di dalam nod

**Manage** pada sesebuah nod membuka skrin nod itu sendiri dari dalam satah kawalan ini: papan
pemukanya, kameranya, peristiwanya, dan — bergantung pada jenis perantinya — perantinya, pintunya
atau peraturan amarannya.

Pelayar anda tidak pernah menyambung kepada nod. Semuanya bergerak melalui saluran kawalan yang
didail keluar oleh nod, dan itulah yang menjadikan peranti di tapak jauh di belakang NAT boleh
digunakan dari sini.

## Apa yang anda dapat mengikut jenis nod {#kinds}

**Perakam kamera** memaparkan papan pemuka, kamera, peristiwa dan konsol jauhnya.

**Hab penderia** memaparkan perantinya, peraturan amaran, log amaran, jenis peranti dan
penyediaannya.

**Pengawal pintu** memaparkan pintunya dan aksesnya.

Halaman ini ialah skrin sebenar nod itu, bukan ringkasannya: semuanya dibaca daripada — dan ditulis
kepada — nod itu sendiri.

## Video langsung {#video}

Jubin kamera membawa **paparan langsung gerakan penuh**, direlaikan melalui saluran media selamat
nod dan disiarkan semula ke pelayar anda melalui WebRTC.

Jubin yang tidak dapat mewujudkan WebRTC akan jatuh secara automatik kepada petikan skrin kira-kira
setiap 1.5 saat. Itu paparan yang terhad dan bukan kegagalan, dan selalunya ia rangkaian yang sedang
memberitahu anda sesuatu tentang dirinya.

Untuk segala yang boleh dilakukan oleh nod — skrin yang tidak dipaparkan di sini — **Open node UI**
pergi terus ke antara muka peranti itu sendiri.

## Nod yang menentukan apa yang boleh anda buat {#authorization}

Inilah bahagian yang berbaloi difahami, kerana ia bukan apa yang orang jangkakan.

Kebenaran anda untuk *mencapai* halaman sesebuah nod datang daripada satah kawalan ini. Kebenaran
anda untuk *melakukan sesuatu di sana* dinilai **oleh nod itu sendiri**, terhadap pemberian
[akses nod](users-and-roles#node-access) anda — viewer, operator atau admin.

Jadi penolakan yang anda temui di dalam halaman ini bukan datang dari sini. Pada hab penderia,
contohnya, viewer nampak peranti dan bacaan semasanya; operator turut nampak sejarah telemetri dan
boleh mengakui amaran; admin boleh mengarahkan peranti. Mintalah pemberian yang lebih tinggi pada nod
itu, bukan peranan yang lebih luas di sini.

Jaminan praktikalnya berbaloi dinyatakan dengan jelas: apabila nod menolak sesuatu arahan, **tiada
apa yang dihantar**. Ia menyatakannya, dan peranti itu langsung tidak disentuh.

## Halaman kosong mungkin bermakna tidak dapat dihubungi {#offline}

Jika nod berada di luar talian, halaman ini tiada apa untuk dibaca.

Skrin menyatakannya secara jelas, dan perbezaan ini lebih penting daripada bunyinya: *halaman kosong
di sini bermakna tidak dapat dihubungi, bukan kosong*. Senarai pintu yang tidak memaparkan apa-apa
kerana pengawalnya tercabut kelihatan sama persis dengan pengawal yang tiada pintu, dan hanya satu
daripadanya masalah yang perlu anda tangani hari ini.

## Konsol jauh {#remote}

**Remote** memanggil API nod melalui terowong secara terus.

Ia laluan keluar bagi apa-apa yang tidak diliputi oleh halaman terbenam. Nod tetap menguatkuasakan
kebenarannya sendiri — akses baca sahaja menolak tulisan — jadi konsol itu tidak boleh digunakan
untuk memintas sesuatu pemberian.

Titik akhir penstriman (video langsung, server-sent events) **tidak boleh diterowongkan** dan tidak
akan berfungsi di sini; gunakan jubin kamera atau UI nod itu sendiri untuknya.

## Apabila halaman nod tidak mahu dimuatkan {#troubleshooting}

1. **Nod berada di luar talian.** Semua di atas memerlukannya bersambung.
2. **Anda tiada akses nod**, atau tidak cukup — penolakan itu datang daripada nod.
3. **Ia titik akhir penstriman** dalam konsol jauh, yang tidak dibawa oleh terowong.
4. **Nod dalam talian tetapi perlahan.** Halaman ini ialah panggilan langsung ke mesin lain melalui
   terowong, bukan salinan cache yang disimpan di sini.
