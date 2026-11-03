# Demo del README. Se genera con: vhs assets/demo.tape
#
# Tres tomas, cada una mostrando algo distinto: de dónde salen los datos, otra
# tabla del mismo binario, y el uso desde un agente. Los comandos son cortos a
# propósito: con todas las flags en la línea el texto se sale por la derecha y
# en el vídeo se lee la mitad.
#
# El .tape se commitea junto al .gif: sin él, el GIF es un binario que nadie
# sabe cómo se hizo ni cómo se regenera cuando cambie la salida de un comando.

Output assets/demo.gif

Set Shell "powershell"
Set FontSize 16
Set FontFamily "Cascadia Mono"
Set Width 1100
Set Height 560
Set Padding 24
Set MarginFill "#1e1e2e"
Set BorderRadius 12
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms

# El binario vive en ~/.local/bin, que está en el PATH de bash pero no
# necesariamente en el de PowerShell. Se prepara sin mostrarlo, junto con el
# Clear-Host que borra el prompt inicial con la ruta del repo.
#
# La ruta se arma con $env:USERPROFILE y no con un nombre de usuario
# escrito a mano: este archivo se commitea para que cualquiera pueda
# regenerar la demo, y una ruta absoluta lo haria reproducible solo en
# la maquina donde se grabo.
Hide
Type "$env:PATH += ';$env:USERPROFILE\.local\bin'"
Enter
Type "Clear-Host"
Enter
Show

# Toma 1 — incidencias reales de la DGT, filtradas por carretera.
# --limit porque A-7 publica 39 ahora mismo y en 560px caben veinte líneas:
# sin él, la tabla se sale por abajo y la atribución no llega a verse.
Type "trafico incidents --limit 5 --road A-7"
Enter
Sleep 6s

Hide
Type "Clear-Host"
Enter
Show

# Toma 2 — otra tabla: los radares fijos, con la provincia resuelta.
# El primer radars de la sesión descarga su feed, así que el Sleep es más
# largo que el de las otras tomas.
Type "trafico radars --limit 5 --road A-2"
Enter
Sleep 7s

Hide
Type "Clear-Host"
Enter
Show

# Toma 3 — el binario explicándose a un agente.
Type "trafico skills"
Enter
Sleep 4s
