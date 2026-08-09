---
title: Mengambil nod
category: fleet
categoryLabel: Armada
summary: Kunci armada, kod tuntutan, dan apa yang perlu dibuat apabila penemuan tidak menjumpai apa-apa.
order: 110
---

# Mengambil nod

Pengambilan ialah satu-satunya prosedur yang merentangi dua produk, itulah sebabnya ia berbaloi
dibaca sekali dan bukan diteka. Ia berlaku dalam tiga gerakan: kongsi kunci, izinkan, ambil.

## 1. Jana kunci armada {#fleet-key}

Jana **kunci armada** di sini, sekali, dan tampalkannya ke dalam setiap peranti yang anda berhasrat
untuk ambil — pada nod MyMataSan itu ialah **Settings → Connectivity**.

Kunci itulah yang menjadikan sesuatu nod boleh ditemui: kuar penemuan ditandatangani dengannya, jadi
**hanya satah kawalan yang memegang kunci yang sama boleh melihat nod itu langsung**. Tanpa kunci,
nod tidak menjawab penemuan.

Itulah sifat keselamatannya, dan ia berfungsi dua hala: layan kunci armada seperti kata laluan.
Sesiapa yang memegangnya boleh menemui dan cuba mengambil peranti anda pada rangkaian tempatan.

## 2. Dapatkan kod tuntutan daripada nod {#claim-code}

Pada nod, jana **kod tuntutan**. Ia berjangka hayat pendek dengan sengaja.

Jabat tangan dua langkah ini disengajakan. Kunci menyatakan *armada mana anda tergolong*; kod
menyatakan *dan saya izinkan sekarang*. Tiada satu pun mencukupi bersendirian, jadi kunci yang
dicuri tidak boleh menyerap peranti orang lain secara senyap.

## 3. Ambil {#adopt}

Kembali di sini, **Discover** mengimbas rangkaian tempatan untuk nod yang belum berpasangan dan
menyenaraikan apa yang menjawab. Pilih nod itu, masukkan kod tuntutannya, dan ambil. Nama lalai
kepada nama hos nod — namakan semula kepada sesuatu yang orang sebut dengan lisan.

Nod yang penemuan tidak dapat lihat boleh diambil **melalui alamat** sebaliknya, iaitu laluan biasa
untuk subnet yang berlainan.

## Satu satah kawalan, secara kekal {#single-parent}

Nod yang telah diambil **dikunci kepada satu satah kawalan** dan berhenti menjawab penemuan
sepenuhnya.

Itulah yang menghalang satah kawalan kedua pada rangkaian yang sama daripada senyap-senyap mengambil
peranti yang sudah menjadi milik orang lain. Tiada keadaan "diambil oleh dua armada", dan inilah
juga sebabnya nod yang sudah diambil tidak pernah muncul dalam Discover.

## Apabila penemuan tidak menjumpai apa-apa {#troubleshooting}

Mengikut kebarangkalian:

1. **Nod tiada kunci armada**, atau kunci yang berlainan. Penemuan senyap secara reka bentuk tanpa
   kunci yang sepadan — inilah punca paling lazim dengan jarak yang jauh.
2. **Nod sudah diambil**, oleh satah kawalan ini atau yang lain. Ia telah senyap. Semak halaman
   Connectivity-nya sendiri.
3. **Nod berada pada subnet atau VLAN lain.** Penemuan menggunakan multicast, yang penghala tidak
   majukan secara lalai. Ambil melalui alamat sebaliknya.
4. **Kod tuntutan telah luput.** Jana yang baharu.
5. **Tembok api sedang menggugurkan trafik penemuan.**

Nod yang penemuan tidak dapat lihat bukanlah nod yang anda tidak boleh ambil. Penemuan ialah
kemudahan; pengambilan melalui alamat sentiasa berfungsi.

## Melepaskan nod {#releasing}

**Lepaskan** ia dari sini — laluan kemas, yang mengekalkan rekod satah kawalan ini konsisten.

Jika satah kawalan ini sudah tiada atau tidak boleh dicapai, nod boleh **gugur sendiri** dari
halaman Connectivity-nya sendiri sebaliknya. Apa cara sekalipun nod itu menjadi boleh ditemui semula
dan satah kawalan ini kehilangan capaian. Jika sesuatu nod gugur sendiri, bersihkan entri usang di
sini.

## Selepas pengambilan {#after}

Kamera, kesihatan dan amaran nod mula digulung serta-merta. Dua perkara berbaloi dilakukan
seterusnya:

- **Letakkannya pada peta** — di tapaknya, dan pada pelan lantai jika anda ada satu. Selagi belum,
  ia hanyalah satu baris dalam senarai dan bukan sebuah tempat.
- **Semak siapa yang boleh melihatnya.** Capaian kepada sesuatu nod diberikan setiap peranan, jadi
  nod yang baharu diambil tidak automatik kelihatan kepada semua orang.

## Port, dan mengapa tapak jauh berfungsi {#ports}

Trafik armada menggunakan portnya sendiri, berasingan daripada antara muka web: penemuan ialah
multicast pada rangkaian tempatan, dan saluran nod-ke-satah-kawalan disahkan secara bersama dengan
sijil yang dikeluarkan oleh pihak berkuasa armada itu sendiri.

Nod **mendail keluar**. Nod di sebalik NAT di tapak jauh oleh itu tidak memerlukan pemajuan port
masuk, yang biasanya menjadi penentu sama ada tapak itu boleh diurus dari jauh langsung.
