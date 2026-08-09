---
title: Menyambung ke satah kawalan
category: administration
categoryLabel: Pentadbiran
summary: Gandingkan peranti ini sebagai nod armada myseliasan, dan nyahgandingkannya semula.
order: 570
---

# Menyambung ke satah kawalan

Peranti tunggal tidak memerlukan apa-apa daripada ini. Langkau keseluruhan halaman melainkan anda
menjalankan **myseliasan**, satah kawalan armada yang menguruskan banyak perakam daripada satu skrin.

Segala yang di sini berada dalam **Tetapan → Ketersambungan**, dan bestari larian pertama menawarkan
langkah yang sama.

## Apa yang diberi gandingan {#why}

Nod yang bergandingan boleh diuruskan daripada satah kawalan: kamera, kesihatan dan amarannya
digulung secara berpusat, dan halamannya boleh dibuka dari sana melalui terowong selamat — pelayar
operator tidak perlu mencapai nod itu secara terus.

Nod tetap berjalan sama sahaja. Gandingan menambah pengurusan berpusat; ia tidak memindahkan rakaman,
pengesanan atau rakaman video ke mana-mana.

## Kunci armada {#fleet-key}

Tampal **kunci armada** yang dijana satah kawalan anda dan simpannya. Minimum enam belas aksara.

Kunci itulah yang menjadikan nod boleh ditemui: dugaan penemuan ditandatangani dengannya, jadi
**hanya satah kawalan yang memegang kunci yang sama boleh melihat atau mengambil nod ini**. Tanpa
kunci, nod langsung tidak menjawab penemuan.

Itulah sifat keselamatannya. Layan kunci armada seperti kata laluan — sesiapa yang memilikinya boleh
menemui dan cuba mengambil nod anda pada rangkaian tempatan.

Memasukkan kunci baharu menggantikan yang sedia ada.

## Kod tuntutan {#claim-code}

Setelah kunci disimpan, **jana kod tuntutan** dan masukkannya dalam satah kawalan semasa mengambil
nod ini. Kod tamat tempoh, dan halaman menunjukkan bila.

Jabat tangan dua langkah ini disengajakan: kunci menyatakan *armada mana anda tergolong*, kod
menyatakan *dan saya bersetuju sekarang*. Tiada satu pun sahaja mencukupi untuk mengambil sesuatu
nod.

## Satu induk sahaja {#single-parent}

Nod yang bergandingan **dikunci kepada satu satah kawalan** dan berhenti menjawab dugaan penemuan
sepenuhnya.

Itulah yang menghalang satah kawalan kedua pada rangkaian yang sama daripada senyap-senyap mengambil
nod yang sudah dimiliki orang lain. Tiada keadaan "bergandingan dengan dua armada".

Tab Ketersambungan menunjukkan induk yang bergandingan dan sejak bila.

## Menyahgandingkan {#unpairing}

Dua laluan, dan kedua-duanya berfungsi:

- **Lepaskannya daripada satah kawalan** — cara biasa, dan yang mengekalkan rekod satah kawalan itu
  kemas.
- **Nyahgandingkan (self-drop)** di sini pada nod — untuk apabila satah kawalan itu telah hilang,
  tidak boleh dicapai, atau dinyahtauliah tanpa melepaskan nodnya.

Apa jua caranya, nod menjadi boleh ditemui semula dan induk sebelumnya kehilangan capaian.

Self-drop ialah jalan keluar. Gunakannya apabila laluan kemas tidak tersedia, dan ingat untuk
membersihkan entri basi pada satah kawalan jika ia kembali.

## Apabila pengambilan tidak berjaya {#troubleshooting}

1. **Adakah kunci armada disimpan pada nod?** Tanpanya, penemuan senyap secara reka bentuk dan satah
   kawalan tidak akan sekali-kali melihatnya.
2. **Adakah kedua-dua pihak mempunyai kunci yang sama?** Kunci yang dijana semula pada satah kawalan
   mesti ditampal di sini semula.
3. **Adakah nod itu sudah bergandingan?** Nod yang bergandingan tidak menjawab penemuan. Semak tab
   Ketersambungan, dan nyahgandingkan jika ia terikat kepada satah kawalan yang tidak lagi anda guna.
4. **Adakah kod tuntutan telah tamat tempoh?** Jana yang baharu — ia berumur pendek atas tujuan.
5. **Adakah penemuan sampai merentasi rangkaian?** Ia menggunakan multicast, yang tidak dimajukan
   penghala secara lalai. Nod pada VLAN yang berbeza daripada satah kawalan tidak akan ditemui.

## Port {#ports}

Trafik armada menggunakan portnya sendiri, berbeza daripada antara muka web: penemuan ialah multicast
pada rangkaian tempatan, dan saluran nod-ke-induk disahkan secara bersama dengan sijil yang
dikeluarkan pihak berkuasa armada itu sendiri.

Nod mendail keluar kepada induk dan bukan sebaliknya, jadi nod di sebalik NAT berfungsi tanpa
pemajuan port masuk — dan itu biasanya faktor penentu bagi tapak jauh.
