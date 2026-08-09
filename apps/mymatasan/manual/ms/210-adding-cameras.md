---
title: Menambah kamera
category: cameras
categoryLabel: Kamera
summary: Temui kamera pada rangkaian atau tambah satu mengikut alamat, dan lepasi pintu kelayakan.
order: 210
---

# Menambah kamera

Entri Kamera pada rel membuka halaman penemuan; setiap kamera di bawahnya membuka halaman kamera itu
sendiri.

## Mengimbas rangkaian {#scan}

**Imbas rangkaian** mencari kamera ONVIF dan menyenaraikan yang menjawab, menunjukkan yang mana sudah
disimpan dan yang mana belum.

Secara lalai ia mengenal pasti subnet tempatan anda dan mengimbasnya. Anda boleh menghalakannya ke
tempat tertentu dalam notasi CIDR sebaliknya:

```
192.168.1.0/24   imbas 192.168.1.1 hingga .254
10.10.20.0/24    imbas satu VLAN
```

Tetapkan **subnet** secara eksplisit apabila kamera anda berada pada VLAN yang berbeza daripada
peranti, atau apabila peranti mempunyai beberapa antara muka rangkaian dan pengesanan automatik
memilih yang salah. **Had masa imbasan** wajar dinaikkan pada rangkaian yang besar atau perlahan —
kamera yang menjawab lewat kelihatan sama seperti kamera yang tiada.

## Apabila imbasan tidak menemui apa-apa {#scan-empty}

Mengikut urutan kebarangkalian:

- **Kamera berada pada subnet atau VLAN lain.** Penemuan menggunakan multicast, yang tidak
  dimajukan oleh penghala secara lalai. Masukkan subnet secara manual, atau tambah kamera mengikut
  alamat.
- **Penemuan ONVIF dimatikan pada kamera.** Banyak kamera dihantar dengan ia dimatikan. Hidupkannya
  dalam antara muka web kamera, atau tambah mengikut alamat.
- **Tembok api menggugurkan trafik penemuan.**
- **Kamera itu langsung tidak menggunakan ONVIF.** Banyak kamera hanya bercakap RTSP. Tambahkannya
  mengikut alamat.

Kamera yang tidak dapat ditemui imbasan bukanlah kamera yang tidak boleh digunakan peranti. Penemuan
ialah kemudahan, bukan keperluan.

## Menambah mengikut alamat {#by-address}

Menduga alamat tertentu dan bukan mengimbas. Ini laluan yang boleh dipercayai, dan yang patut dicapai
setiap kali penemuan menyusahkan — VLAN berbeza, kamera dicapai melalui NAT, atau kamera yang
penemuan ONVIFnya dimatikan.

Anda perlukan alamat kamera dan kelayakannya. Jika kamera bercakap ONVIF, peranti akan mengenal pasti
URL strimnya sendiri; jika tidak, berikan URL RTSP.

## Kelayakan {#credentials}

Hampir setiap kamera memerlukan nama pengguna dan kata laluan untuk menstrim. Masukkan kelayakan
kamera itu sendiri — yang anda gunakan dalam antara muka webnya — bukan mana-mana akaun pada peranti
ini.

**Kelayakan disahkan sebelum kamera disimpan.** Kata laluan yang salah gagal di sini, serta-merta,
dengan mesej. Ini disengajakan: alternatifnya ialah kamera yang kelihatan tersimpan dengan baik dan
kemudian memaparkan jubin hitam beberapa jam kemudian, apabila tiada sesiapa ingat apa yang ditaip.

Jika kamera kemudiannya berhenti mengesahkan — seseorang menukar kata laluannya — halamannya
tersekat sehingga kelayakan yang disimpan berfungsi semula. Betulkannya pada tab **Akses** kamera.

> [!TIP]
> Berikan peranti akaunnya sendiri pada setiap kamera dan bukan berkongsi log masuk admin. Apabila
> kata laluan perlu berubah, anda menukar satu perkara di satu tempat, dan anda boleh melihat dalam
> log kamera itu sendiri capaian mana yang datang daripada perakam.

## Menamakan {#naming}

Namakan kamera seperti seseorang akan menyebutnya melalui radio: *Pintu Depan*, *Ruang Muatan*,
*Tempat Letak Kereta Utara*. Bukan `CAM-04`, dan bukan nombor model.

Setiap amaran, setiap pemberitahuan dan setiap rakaman membawa nama ini, dan inilah yang dibaca
seseorang pada pukul 3 pagi. Menamakan semula kemudian adalah mudah — ia satu medan pada tab
Butiran kamera — jadi tiada kos untuk membetulkan nama yang buruk.

## Selepas menambah {#after}

Kamera yang baru ditambah tidak melakukan apa-apa dengan sendirinya. Ia kelihatan dalam paparan
langsung; ia tidak merakam, dan ia tidak mengesan apa-apa.

Dua langkah lagi menjadikannya berguna:

1. Hidupkan rakaman — [Konfigurasi rakaman](recording-configuration).
2. Tambah sekurang-kurangnya satu peraturan pengesanan —
   [Mencipta peraturan pengesanan](detection-rules).

## Membuang kamera {#removing}

Membuang kamera memadamnya dan segala yang dikonfigurasikan padanya: strimnya, konfigurasi
rakamannya dan peraturan AInya. Ia tidak boleh dibatalkan, dan anda menaip nama kamera untuk
mengesahkan.

Pembuangan juga merupakan jalan keluar apabila kata laluan kamera telah hilang — anda tidak boleh
membuka halamannya tanpa kelayakan yang berfungsi, tetapi anda sentiasa boleh membuangnya dan
menambahnya semula.
