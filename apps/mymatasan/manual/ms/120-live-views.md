---
title: Paparan Langsung
category: daily-use
categoryLabel: Penggunaan harian
summary: Menyusun dinding video, serta menggunakan audio, cakap-balas dan PTZ pada kamera.
order: 120
---

# Paparan Langsung

Paparan Langsung ialah dinding video: kamera anda disusun dalam grid yang anda atur.

## Menyusun dinding {#layout}

Pilih saiz grid, kemudian seret jubin untuk menyusun semula. Jika kamera anda lebih banyak daripada
muatan grid, yang selebihnya dihalamankan dan bukan digugurkan — grid ialah saiz halaman, bukan had.

Susun atur diingati dalam pelayar ini, bagi setiap orang. Dua operator pada dua mesin masing-masing
boleh mengekalkan susunan yang sesuai dengan tugas mereka.

## Maksud status jubin {#tile-status}

Setiap jubin melaporkan cara ia dihantar, dan perkataannya penting apabila anda sedang mendiagnosis
sesuatu:

| Penunjuk | Maksudnya |
|---|---|
| **Langsung** | Video WebRTC terus. Laluan cekap — pelayar menyahkod strim kamera itu sendiri. |
| **Menyambung** / **Menyambung semula…** | Sedang berunding, atau pulih selepas terputus. Sekejap adalah normal. |
| **MJPEG** / **Sandaran MJPEG** | Peranti menukar bingkai menjadi imej pegun untuk pelayar. Berfungsi di mana-mana, jauh lebih mahal dari segi CPU bagi setiap kamera. |
| **WebRTC perlukan H264** | Kamera menstrim kodek yang pelayar tidak boleh main terus, jadi main balik berundur. Tukar strim kamera kepada H.264 untuk mendapatkan semula laluan cekap. |
| **Kamera luar talian** | Tidak dapat dicapai sekarang. Lihat [Kesihatan kamera](camera-health). |
| **Paparan langsung dimatikan** | Dimatikan untuk kamera ini pada halamannya sendiri. |

Dinding yang keseluruhannya MJPEG akan membebankan mesin jauh lebih berat berbanding dinding yang
sama pada WebRTC. Jika jubin tersekat-sekat, semak lajur ini sebelum anda menyalahkan perkakasan.

## Audio {#audio}

Jubin dibisukan secara lalai — bilik kawalan dengan sedozen kamera yang tidak dibisukan tidak boleh
digunakan. Nyahbisukan yang anda benar-benar sedang dengar.

Jika kodek audio kamera ialah yang tidak boleh dimainkan pelayar, peranti akan mentranskodkannya. Itu
memerlukan CPU bagi setiap jubin yang didengar, jadi ia satu lagi sebab untuk menyahbisukan secara
sengaja dan bukan secara lalai.

## Cakap-balas {#talk-back}

Di mana kamera mempunyai pembesar suara, butang mikrofon bercakap melaluinya. Ia memerlukan kebenaran
daripada pelayar anda untuk menggunakan mikrofon, dan ia memerlukan kata laluan yang betul disimpan
pada tab **Akses** kamera itu.

Kata laluan mana bergantung pada kamera, dan ini kerap mengelirukan orang:

- **Kamera ONVIF standard** menggunakan kelayakan kamera yang disimpan. Tiada apa-apa lagi.
- Kamera **TP-Link Tapo** menggunakan **kata laluan akaun awan TP-Link** anda — yang anda gunakan
  untuk log masuk ke aplikasi Tapo — bukan kata laluan strim kamera.
- Kamera **TP-Link VIGI** menggunakan kata laluan admin kamera.

Jika cakap-balas ditolak, tab Akses kamera mempunyai senarai semak untuk hal ini. Punca paling biasa
ialah kes Tapo di atas; yang kedua paling biasa ialah akaun TP-Link yang dilog masuk melalui Google
atau Apple, yang tiada kata laluan untuk digunakan sehingga anda menetapkan satu.

## Pan, tilt dan zum {#ptz}

Pada kamera yang menyokongnya, kawalan PTZ muncul pada jubin. Ia tersedia untuk operator dan
pentadbir, bukan untuk pemerhati.

Menggerakkan kamera mengubah apa yang dilihat oleh setiap peraturan padanya. Zon pengesanan yang
dilukis ialah kawasan pada bingkai, bukan kawasan di dunia — pan kamera itu dan zon itu kini menutup
tempat lain. Jika anda memindahkan kamera secara kekal, semak semula peraturannya.

## Menambah dan membuang jubin {#tiles}

Tambah kamera ke dinding daripada halamannya sendiri atau daripada kawalan grid; buang jubin dengan
kawalan pada jubin itu. Membuang jubin hanya mengubah paparan anda — ia tidak menghentikan rakaman,
pengesanan atau amaran, yang semuanya berjalan pada peranti tanpa mengira sama ada ada sesiapa yang
memerhati.

## Dinding yang disimpan {#walls}

Susunan yang berbaloi disimpan ialah **dinding**: satu nama, satu grid, dan kamera mengikut susunan
yang anda tetapkan. Simpan satu dan ia tersedia kepada semua orang, pada setiap mesin — termasuk
operator syif berikutnya, sesuatu yang susunan per-pelayar lama tidak pernah lakukan.

Guna **Simpan dinding** untuk mengemas kini dinding yang sedang anda lihat, atau **Simpan sebagai
baharu** untuk menyimpan grid semasa dengan nama baharu. Satu dinding boleh ditandakan sebagai
lalai; itulah yang dibuka oleh skrin.

> [!NOTE]
> Dinding ialah milik bersama, bukan peribadi. Mengubahnya mengubahnya untuk semua orang yang
> membukanya. Itu disengajakan — dinding sesebuah bilik kawalan ialah perabot, dan pentadbir mesti
> boleh membetulkan dinding yang sedang dilihat semua orang.

Jika kamera pada dinding dipadam, dinding itu menyatakannya dan bukan diam-diam memaparkan satu
jubin kurang. Simpan dinding itu semula untuk melupakan kamera yang hilang.

## Kitaran dan paparan amaran {#cycle}

Dua nombor pada bar dinding menentukan apa yang dilakukan dinding semasa tiada sesiapa
menyentuhnya:

- **Kitaran (s)** memutar halaman setiap beberapa saat, supaya satu stesen boleh memantau lebih
  banyak kamera daripada muat dalam grid. 0 membiarkan dinding pegun.
- **Papar amaran (s)** membawa satu kamera ke halaman yang kelihatan apabila ia membangkitkan
  amaran, dan menahannya di situ selama saat tersebut. 0 tidak pernah berbuat demikian.

Semasa kamera yang dipaparkan itu kelihatan, kitaran **dijeda**. Tujuan paparan itu ialah supaya
seseorang melihatnya, dan dinding yang berputar pergi dua saat kemudian melakukan yang sebaliknya
daripada membangkitkan penggera. Jubin yang dipaparkan itu digariskan supaya jelas kamera mana yang
menyebabkan dinding melompat.

Menukar halaman secara manual memulakan semula tempoh menunggu, jadi halaman yang anda buka tidak
ditarik pergi sebentar kemudian.

## Monitor kedua {#second-monitor}

**Monitor kedua** membuka dinding yang dipilih dalam tetingkapnya sendiri tanpa navigasi dan tanpa
apa-apa lagi pada skrin. Seret ia ke paparan yang satu lagi dan jadikan skrin penuh.

Ia memang satu tetingkap pelayar sebenar: panel di dalam aplikasi tidak boleh diseret ke skrin lain.
Ia log masuk sama seperti mana-mana tetingkap lain, dan ia memaparkan dinding yang sama — jadi
mengubah dinding mengubah kedua-duanya.
